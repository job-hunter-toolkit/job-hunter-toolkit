// store.mjs — unit test of corpus-store.js's size probe and fallback logic,
// under Node with a stubbed fetch. Run with: node web/test/store.mjs
//
// The fetch stub is browser-faithful about CORS header visibility, because the
// launch-day outage was exactly that gap: raw.githubusercontent.com honours
// Range and sends Content-Range, but without Access-Control-Expose-Headers a
// browser's fetch cannot READ Content-Range, while Content-Length is
// CORS-safelisted and always readable. Each host below models one combination
// of {answers HEAD, honours Range, exposes Content-Range}, and the assertions
// pin the path the page takes on each.

import { createStore, parseContentRange } from "../corpus-store.js";
import { exit } from "node:process";

let failures = 0;

function check(what, got, want) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures += 1;
    console.error(`FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  } else {
    console.log(`ok   ${what}`);
  }
}

const body = new Uint8Array(1000);
for (let i = 0; i < body.length; i++) body[i] = i % 251;

const manifest = new TextEncoder().encode(`{"format_version":1}`);

function respond(bytes, status, headers = {}) {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (k) => headers[k] ?? null },
    arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
  };
}

function makeFetch({ head, range, exposeContentRange }, log) {
  return async (url, options = {}) => {
    const method = options.method ?? "GET";
    const wantRange = options.headers?.Range;
    log.push({ url: url.toString(), method, range: wantRange ?? null });

    if (url.toString().endsWith("manifest.json")) {
      return respond(manifest, 200);
    }

    if (method === "HEAD") {
      // Content-Length is CORS-safelisted: a host that answers HEAD at all
      // always yields a readable length.
      return head ? respond(new Uint8Array(0), 200, { "Content-Length": String(body.length) }) : respond(new Uint8Array(0), 405);
    }

    if (wantRange && range) {
      const match = /^bytes=(\d+)-(\d+)$/.exec(wantRange);
      const [start, end] = [Number(match[1]), Number(match[2])];
      const slice = body.subarray(start, Math.min(end + 1, body.length));
      const headers = exposeContentRange
        ? { "Content-Range": `bytes ${start}-${start + slice.length - 1}/${body.length}` }
        : {};

      return respond(slice, 206, headers);
    }

    return respond(body, 200);
  };
}

// --- content-range parsing --------------------------------------------------

check("content-range total", parseContentRange("bytes 0-0/12345"), 12345);
check("content-range star is refused", parseContentRange("bytes 0-0/*"), null);
check("content-range garbage is refused", parseContentRange("chunks 1-2/3"), null);

// --- raw.githubusercontent as a browser sees it: HEAD ok, ranges honoured,
// --- Content-Range invisible. The launch-day regression test. ---------------

{
  const log = [];
  const store = await createStore(
    "https://host.example/corpus/",
    makeFetch({ head: true, range: true, exposeContentRange: false }, log),
  );

  check("raw: size via HEAD Content-Length", await store.size("corpus.jhtc"), 1000);
  check("raw: mode", store.stats.mode, "range");

  const chunk = await store.readAt("corpus.jhtc", 500, 10);
  check("raw: readAt bytes", Array.from(chunk), Array.from(body.subarray(500, 510)));
  check("raw: never fetched whole", store.stats.bytesFetched < body.length, true);
  check(
    "raw: no Range probe needed",
    log.filter((r) => r.url.endsWith("corpus.jhtc")).map((r) => `${r.method}${r.range ? " " + r.range : ""}`),
    ["HEAD", "GET bytes=500-509"],
  );
}

// --- host that refuses HEAD but ranges with an exposed Content-Range --------

{
  const log = [];
  const store = await createStore(
    "https://host.example/corpus/",
    makeFetch({ head: false, range: true, exposeContentRange: true }, log),
  );

  check("probe: size via 1-byte range", await store.size("corpus.jhtc"), 1000);
  check("probe: mode", store.stats.mode, "range");

  const chunk = await store.readAt("corpus.jhtc", 500, 10);
  check("probe: readAt bytes", Array.from(chunk), Array.from(body.subarray(500, 510)));
  check(
    "probe: requests sent",
    log.filter((r) => r.range).map((r) => r.range),
    ["bytes=0-0", "bytes=500-509"],
  );
}

// --- host that refuses HEAD and 206s without a readable Content-Range -------

{
  const log = [];
  const store = await createStore(
    "https://host.example/corpus/",
    makeFetch({ head: false, range: true, exposeContentRange: false }, log),
  );

  check("blind-206: size falls back to whole fetch", await store.size("corpus.jhtc"), 1000);

  const chunk = await store.readAt("corpus.jhtc", 990, 10);
  check("blind-206: readAt bytes", Array.from(chunk), Array.from(body.subarray(990, 1000)));
}

// --- host that ignores Range entirely ----------------------------------------

{
  const log = [];
  const store = await createStore(
    "https://host.example/corpus/",
    makeFetch({ head: true, range: false, exposeContentRange: false }, log),
  );

  check("whole: size via HEAD", await store.size("corpus.jhtc"), 1000);

  const chunk = await store.readAt("corpus.jhtc", 990, 10);
  check("whole: readAt bytes", Array.from(chunk), Array.from(body.subarray(990, 1000)));
  check("whole: degraded mode after 200", store.stats.mode, "whole-file");

  await store.readAt("corpus.jhtc", 0, 100);
  const gets = log.filter((r) => r.url.endsWith("corpus.jhtc") && r.method === "GET");
  check("whole: exactly one GET of the object", gets.length, 1);

  const overrun = await store.readAt("corpus.jhtc", 999, 10).then(
    () => "no error",
    (err) => err.message.includes("beyond"),
  );
  check("whole: overrun read errors", overrun, true);
}

// --- small objects are always fetched whole ---------------------------------

{
  const log = [];
  const store = await createStore(
    "https://host.example/corpus/",
    makeFetch({ head: true, range: true, exposeContentRange: true }, log),
  );

  await store.size("manifest.json");
  check("manifest fetched without Range", log.every((r) => r.range === null || r.url.endsWith(".jhtc")), true);
}

if (failures > 0) {
  console.error(`${failures} failure(s)`);
  exit(1);
}

console.log("store test passed");
