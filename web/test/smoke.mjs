// smoke.mjs — end-to-end test of the compiled wasm engine under Node.
//
//   node web/test/smoke.mjs <site-dir> <corpus-dir>
//
// This drives exactly the surface the page uses — jhtEngine.open / load /
// search against a store of size()/readAt() promises — with the store backed
// by the local filesystem instead of fetch. It is the closest this repo can
// get to a browser test without a browser (this container has no browser
// egress), and it leaves only the DOM layer and real HTTP unexercised.
//
// Node is the right stand-in for the engine itself: V8 is the engine Chrome
// ships, and the wasm binary is byte-identical to the one the page loads.

import { readFileSync, openSync, readSync, closeSync, statSync } from "node:fs";
import { join } from "node:path";
import { argv, exit } from "node:process";

const [siteDir, corpusDir] = argv.slice(2);

if (!siteDir || !corpusDir) {
  console.error("usage: node web/test/smoke.mjs <site-dir> <corpus-dir>");
  exit(2);
}

// wasm_exec.js is a classic script that installs globalThis.Go.
(0, eval)(readFileSync(join(siteDir, "wasm_exec.js"), "utf8"));

const store = {
  reads: 0,
  bytes: 0,
  async size(name) {
    return statSync(join(corpusDir, name)).size;
  },
  async readAt(name, off, len) {
    const fd = openSync(join(corpusDir, name), "r");

    try {
      const buf = new Uint8Array(len);
      const n = readSync(fd, buf, 0, len, off);
      this.reads += 1;
      this.bytes += n;

      if (n !== len) throw new Error(`${name}: short read ${n} of ${len} at ${off}`);

      return buf;
    } finally {
      closeSync(fd);
    }
  },
};

let failures = 0;

function check(what, got, want) {
  const ok = JSON.stringify(got) === JSON.stringify(want);

  if (!ok) {
    failures += 1;
    console.error(`FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  } else {
    console.log(`ok   ${what}: ${JSON.stringify(got)}`);
  }
}

const go = new Go();
const ready = new Promise((resolve) => {
  globalThis.jhtEngineReady = resolve;
});

const wasm = readFileSync(join(siteDir, "engine.wasm"));
const { instance } = await WebAssembly.instantiate(wasm, go.importObject);
go.run(instance); // runs forever; not awaited
await ready;

// --- open ------------------------------------------------------------------

const summary = JSON.parse(await jhtEngine.open(store));
const openReads = store.reads;
const openBytes = store.bytes;
console.log(`open: ${openReads} reads, ${openBytes} bytes (manifest + sources + runs + footer)`);

// The fixture's promises, from web/internal/testcorpus. NOTE: the engine
// computes lifecycle states against the real clock, so state *math* against
// the pinned fixture clock is asserted natively in web/engine's tests; here
// the manifest counts (written at fold time) pin the corpus content, and the
// searches below use predicates that are clock-independent.
check("rows", summary.rows, 8);
check("manifest open", summary.open, 4);
check("manifest closed", summary.closed, 1);
check("manifest lapsed", summary.lapsed, 1);
check("generation", summary.generation, 3);
check("partial", summary.partial, false);

// --- load ------------------------------------------------------------------

const t0 = performance.now();
const stats = JSON.parse(await jhtEngine.load());
const loadMS = performance.now() - t0;
check("loaded rows", stats.rows, 8);
console.log(`load: ${loadMS.toFixed(0)} ms, ${store.reads - openReads} reads, ${store.bytes - openBytes} bytes`);

// --- search ----------------------------------------------------------------

// The engine here runs against the real clock, so open-vs-stale for the
// fixture's rows drifts as real time passes its pinned instants. Closed and
// lapsed rows are written at fold time and cannot drift for 90 days, and
// include_closed:true sees every row regardless of state — so all matched
// counts below use it to stay clock-independent. The state *math* against a
// pinned clock is asserted natively in web/engine's tests.
const search = async (request) => JSON.parse(await jhtEngine.search(JSON.stringify(request)));

const all = await search({ include_closed: true });
check("include_closed matches", all.matched, 8);
check("closed rows visible", all.states.closed, 1);
check("lapsed rows visible", all.states.lapsed, 1);

check("title substring", (await search({ include_closed: true, titles: ["engineer"] })).matched, 4);
check(
  "title + exclude",
  (await search({ include_closed: true, titles: ["engineer"], exclude_titles: ["security"] })).matched,
  3,
);
check("company", (await search({ include_closed: true, companies: ["globex"] })).matched, 2);
check("remote heuristic", (await search({ include_closed: true, remote: true })).matched, 2);
check("pay floor", (await search({ include_closed: true, min_annual: 200000 })).matched, 1);
check(
  "employment type, board spelling",
  (await search({ include_closed: true, employment_types: ["Full-Time"] })).matched,
  2,
);
check("workplace type", (await search({ include_closed: true, workplace_types: ["remote"] })).matched, 2);
check("department", (await search({ include_closed: true, departments: ["security"] })).matched, 1);

const faceted = await search({ include_closed: true, include_facets: true, titles: ["engineer"] });
check("facet count unit", faceted.count_unit, "rows");
check("facets follow filters", faceted.facets.employment.reduce((sum, facet) => sum + facet.rows, 0), faceted.matched);
check("employment facet", faceted.facets.employment.find((facet) => facet.value === "full_time").rows, 2);
check("workplace facet", faceted.facets.workplace.find((facet) => facet.value === "remote").rows, 2);
check("facets are omitted by default", Object.hasOwn(all, "facets"), false);

const paged = await search({ include_closed: true, limit: 3, offset: 6 });
check("paging window", paged.items.length, 2);
check("paging total", paged.matched, 8);

const first = (await search({ include_closed: true, titles: ["senior software"] })).items[0];
check("compensation label", first.compensation, "150,000–180,000 USD / year");
check("item url", first.url, "https://example.com/acme/jobs/1");

const bad = await jhtEngine.search(JSON.stringify({ employment_types: ["gibberish"] })).then(
  () => "no error",
  (err) => err.message,
);
check("unknown enum rejects", bad.includes("unknown employment type"), true);

const memory = process.memoryUsage();
console.log(`heap after: ${(memory.heapUsed / 1048576).toFixed(1)} MiB JS heap, ${(instance.exports.mem?.buffer.byteLength ?? 0) / 1048576} MiB wasm memory`);

if (failures > 0) {
  console.error(`${failures} failure(s)`);
  exit(1);
}

console.log("smoke test passed");
exit(0);
