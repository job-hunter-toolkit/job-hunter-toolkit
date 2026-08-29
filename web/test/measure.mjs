// measure.mjs — measures the wasm engine at corpus volume under Node.
//
//   go run ./web/fixture -dir <corpus-dir> -scale 100000   # ~800k rows
//   node web/test/measure.mjs <site-dir> <corpus-dir>
//
// Prints load time, per-query time and memory. This is the harness behind the
// numbers quoted in web/README.md; it asserts nothing about counts because
// the scaled fixture's match counts are properties of the synthesised corpus,
// not of anything real.

import { readFileSync, openSync, readSync, closeSync, statSync } from "node:fs";
import { join } from "node:path";
import { argv, exit } from "node:process";

const [siteDir, corpusDir] = argv.slice(2);

if (!siteDir || !corpusDir) {
  console.error("usage: node web/test/measure.mjs <site-dir> <corpus-dir>");
  exit(2);
}

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

      if (n !== len) throw new Error(`${name}: short read`);

      return buf;
    } finally {
      closeSync(fd);
    }
  },
};

const go = new Go();
const ready = new Promise((resolve) => {
  globalThis.jhtEngineReady = resolve;
});
const wasm = readFileSync(join(siteDir, "engine.wasm"));
const { instance } = await WebAssembly.instantiate(wasm, go.importObject);
go.run(instance);
await ready;

const mib = (n) => (n / 1048576).toFixed(1);

const summary = JSON.parse(await jhtEngine.open(store));
console.log(`corpus: ${summary.rows} rows, ${summary.sources} sources, ${mib(statSync(join(corpusDir, "corpus.jhtc")).size)} MiB .jhtc`);

let t = performance.now();
const stats = JSON.parse(await jhtEngine.load());
console.log(`load: ${(performance.now() - t).toFixed(0)} ms wall (${stats.elapsed_ms} ms engine), ${store.reads} reads, ${mib(store.bytes)} MiB fetched`);

const queries = [
  ["title substring", { titles: ["engineer"] }],
  ["title+remote+pay floor", { titles: ["engineer"], remote: true, min_annual: 150000 }],
  ["employment type", { employment_types: ["full_time"] }],
  ["match everything", {}],
  ["deep page", { offset: 100000, limit: 100 }],
  ["faceted overview", { include_facets: true }],
];

for (const [name, request] of queries) {
  t = performance.now();
  const response = JSON.parse(await jhtEngine.search(JSON.stringify(request)));
  console.log(`search ${name}: ${(performance.now() - t).toFixed(1)} ms, ${response.matched} matched`);
}

console.log(`memory: ${mib(process.memoryUsage().rss)} MiB rss, ${mib(instance.exports.mem?.buffer.byteLength ?? 0)} MiB wasm linear memory`);
exit(0);
