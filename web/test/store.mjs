// store.mjs — unit test of corpus-store.js's Range probe and fallback logic,
// under Node with a stubbed fetch. Run with: node web/test/store.mjs
//
// The fetch stub plays two hosts: one honouring Range with 206 + a correct
// Content-Range, and one that ignores Range and always answers 200 with the
// whole body. The assertions pin the two behaviours the page depends on: a
// ranging host is read in byte ranges and never fetched whole, and a
// non-ranging host degrades to exactly one whole-file fetch.

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

function makeFetch(supportsRange, log) {
  return async (url, options = {}) => {
    const range = options.headers?.Range;
    log.push({ url: url.toString(), range: range ?? null });

    if (url.toString().endsWith("manifest.json")) {
      return respond(manifest, 200);
    }

    if (range && supportsRange) {
      const match = /^bytes=(\d+)-(\d+)$/.exec(range);
      const [start, end] = [Number(match[1]), Number(match[2])];
      const slice = body.subarray(start, Math.min(end + 1, body.length));

      return respond(slice, 206, {
        "Content-Range": `bytes ${start}-${start + slice.length - 1}/${body.length}`,
      });
    }

    return respond(body, 200);
  };
}

// --- content-range parsing --------------------------------------------------

check("content-range total", parseContentRange("bytes 0-0/12345"), 12345);
check("content-range star is refused", parseContentRange("bytes 0-0/*"), null);
check("content-range garbage is refused", parseContentRange("chunks 1-2/3"), null);

// --- host that honours Range ------------------------------------------------

{
  const log = [];
  const store = await createStore("https://host.example/corpus/", makeFetch(true, log));

  check("range: size via 1-byte probe", await store.size("corpus.jhtc"), 1000);
  check("range: mode", store.stats.mode, "range");

  const chunk = await store.readAt("corpus.jhtc", 500, 10);
  check("range: readAt bytes", Array.from(chunk), Array.from(body.subarray(500, 510)));

  const rangesSent = log.filter((r) => r.range).map((r) => r.range);
  check("range: requests sent", rangesSent, ["bytes=0-0", "bytes=500-509"]);
  check("range: never fetched whole", store.stats.bytesFetched < body.length, true);
}

// --- host that ignores Range ------------------------------------------------

{
  const log = [];
  const store = await createStore("https://host.example/corpus/", makeFetch(false, log));

  check("whole: size", await store.size("corpus.jhtc"), 1000);
  check("whole: mode", store.stats.mode, "whole-file");

  const chunk = await store.readAt("corpus.jhtc", 990, 10);
  check("whole: readAt bytes", Array.from(chunk), Array.from(body.subarray(990, 1000)));

  await store.readAt("corpus.jhtc", 0, 100);
  const objectFetches = log.filter((r) => r.url.endsWith("corpus.jhtc"));
  check("whole: exactly one fetch of the object", objectFetches.length, 1);

  const overrun = await store.readAt("corpus.jhtc", 999, 10).then(
    () => "no error",
    (err) => err.message.includes("beyond"),
  );
  check("whole: overrun read errors", overrun, true);
}

// --- small objects are always fetched whole ---------------------------------

{
  const log = [];
  const store = await createStore("https://host.example/corpus/", makeFetch(true, log));

  await store.size("manifest.json");
  check("manifest fetched without Range", log.every((r) => r.range === null || r.url.endsWith(".jhtc")), true);
}

if (failures > 0) {
  console.error(`${failures} failure(s)`);
  exit(1);
}

console.log("store test passed");
