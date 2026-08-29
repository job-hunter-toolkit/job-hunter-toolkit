// worker.js — the engine's home, off the main thread.
//
// Every expensive thing this app does — fetching 30 MiB of columns, folding
// 1.3 million rows, scanning them per query — happens here, so the page's
// thread never runs anything slower than rendering 100 cards. Before this
// worker existed, the wasm scan ran on the UI thread and a search mid-word
// froze the keystrokes behind it; a search tool where typing lags is broken.
//
// Protocol: the page posts {id, op, args}; the worker answers {id, ok, value}
// or {id, ok: false, error}. Progress stats flow page-ward every 250 ms during
// load. This is a module worker so it can share corpus-store.js with the
// page verbatim; wasm_exec.js is a classic script, so it is fetched and
// evaluated, the same way the Node smoke test loads it.

import { createStore } from "./corpus-store.js";

let store;
let enginePromise;

async function ensureEngine() {
  enginePromise ??= (async () => {
    const shim = await (await fetch("wasm_exec.js")).text();
    (0, eval)(shim);

    const go = new Go();
    const ready = new Promise((resolve) => {
      globalThis.jhtEngineReady = resolve;
    });

    const result = await WebAssembly.instantiateStreaming(fetch("engine.wasm"), go.importObject).catch(async () => {
      // instantiateStreaming requires Content-Type: application/wasm; fall
      // back for hosts that mislabel it.
      const response = await fetch("engine.wasm");

      return WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
    });

    go.run(result.instance); // resolves only if the engine exits; not awaited
    await ready;
  })();

  return enginePromise;
}

const ops = {
  async open({ corpusURL }) {
    await ensureEngine();
    store = await createStore(corpusURL);

    return JSON.parse(await jhtEngine.open(store));
  },

  async load() {
    const ticker = setInterval(() => {
      postMessage({ op: "progress", ...store.stats });
    }, 250);

    try {
      const stats = JSON.parse(await jhtEngine.load());

      return { ...stats, ...store.stats };
    } finally {
      clearInterval(ticker);
    }
  },

  async search({ request, token }) {
    return JSON.parse(await jhtEngine.search(JSON.stringify(request), token));
  },
};

onmessage = async (event) => {
  const { id, op, args } = event.data;

  if (op === "cancel") {
    void jhtEngine.cancel(args.token);
    return;
  }

  try {
    const value = await ops[op](args ?? {});
    postMessage({ id, ok: true, value });
  } catch (err) {
    postMessage({ id, ok: false, error: String(err?.message ?? err) });
  }
};
