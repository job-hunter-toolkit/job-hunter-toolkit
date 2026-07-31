// corpus-store.js — fetches corpus objects over HTTP for the wasm engine.
//
// The engine reads through a Store: `size(name)` and `readAt(name, off, len)`,
// both async, mirroring Go's io.ReaderAt. The whole point of the .jhtc format
// is that one column is one contiguous byte range, so when the host honours
// Range requests the engine's load fetches only the columns it decodes and the
// footer costs two reads of a few kilobytes.
//
// Not every host honours Range, and none is contractually obliged to keep
// doing so, so support is *probed*, never assumed:
//
//   - The first ranged read of an object sends `Range: bytes=n-m`.
//   - A 206 response pins range mode for that object.
//   - A 200 response means the server sent the whole file anyway; the body is
//     kept, and every later read is served from memory. A bigger download,
//     never a broken page.
//
// The small JSON/NDJSON objects (manifest, sources, runs) are always fetched
// whole — they are read in full by the engine regardless, so ranging them
// would only add requests.
//
// This module deliberately touches no DOM and no globals beyond fetch/URL, so
// web/test/store.mjs can exercise the probe logic under Node with a stubbed
// fetch.

// WHOLE_OBJECTS are read in full by the engine, so they are fetched in full.
const WHOLE_OBJECTS = new Set(["manifest.json", "sources.json", "runs.ndjson"]);

// The default binds fetch to globalThis: the store calls it as `this.fetch(…)`,
// and native fetch invoked with any other receiver throws "Illegal invocation"
// in Chrome. Node's test stubs never care about their receiver, which is
// exactly why only a real browser caught this.
export async function createStore(baseURL, fetchImpl = globalThis.fetch.bind(globalThis)) {
  const store = new CorpusStore(baseURL, fetchImpl);

  // Fail fast and clearly: a missing manifest is "no corpus is published
  // here", and that message beats a wasm stack trace from a half-open table.
  await store.whole("manifest.json");

  return store;
}

class CorpusStore {
  constructor(baseURL, fetchImpl) {
    this.baseURL = baseURL;
    this.fetch = fetchImpl;
    this.buffers = new Map(); // name -> Uint8Array, whole objects only
    this.ranged = new Map(); // name -> { size } for objects in range mode
    this.stats = { requests: 0, bytesFetched: 0, mode: "probing" };
  }

  url(name) {
    return new URL(name, new URL(this.baseURL, globalThis.location?.href ?? "http://localhost/")).toString();
  }

  async size(name) {
    if (this.buffers.has(name)) {
      return this.buffers.get(name).byteLength;
    }

    if (this.ranged.has(name)) {
      return this.ranged.get(name).size;
    }

    if (WHOLE_OBJECTS.has(name)) {
      return (await this.whole(name)).byteLength;
    }

    // HEAD first: Content-Length is a CORS-safelisted response header, so a
    // browser can always read it. Content-Range is NOT safelisted, and
    // raw.githubusercontent.com sends no Access-Control-Expose-Headers, so a
    // 206's Content-Range is invisible to cross-origin JS even though curl
    // sees it. The launch-day outage was exactly this: a host that ranges
    // perfectly, and a probe that trusted a header the browser withholds.
    try {
      const head = await this.request(name, {}, "HEAD");
      if (head.ok) {
        const total = Number(head.headers.get("Content-Length"));

        if (Number.isFinite(total) && total > 0) {
          // Whether ranged reads actually work is decided by the first real
          // read in readAt: a 206 keeps range mode, a 200 degrades to
          // whole-file. Nothing here depends on Content-Range.
          this.ranged.set(name, { size: total });
          this.stats.mode = "range";

          return total;
        }
      }
    } catch {
      // Fall through to the GET probe.
    }

    // GET probe with a 1-byte range, for hosts that refuse HEAD. 206 with a
    // readable Content-Range gives the size for one byte; 200 hands us the
    // whole object, which we keep instead of re-fetching.
    const response = await this.request(name, { Range: "bytes=0-0" });

    if (response.status === 206) {
      const total = parseContentRange(response.headers.get("Content-Range"));

      if (total !== null) {
        await response.arrayBuffer(); // drain the byte
        this.stats.bytesFetched += 1;
        this.ranged.set(name, { size: total });
        this.stats.mode = "range";

        return total;
      }

      // 206 with an unreadable Content-Range: the host ranges but exposes
      // nothing. Fetch the object whole rather than fail.
      return (await this.whole(name)).byteLength;
    }

    if (response.status === 200) {
      const bytes = new Uint8Array(await response.arrayBuffer());
      this.stats.bytesFetched += bytes.byteLength;
      this.buffers.set(name, bytes);
      this.stats.mode = "whole-file";

      return bytes.byteLength;
    }

    throw new Error(`${name}: HTTP ${response.status}`);
  }

  async readAt(name, off, len) {
    if (len === 0) {
      return new Uint8Array(0);
    }

    if (!this.buffers.has(name) && !this.ranged.has(name)) {
      await this.size(name); // establishes the object's mode
    }

    if (this.buffers.has(name)) {
      const buf = this.buffers.get(name);
      if (off + len > buf.byteLength) {
        throw new Error(
          `${name}: read [${off}, +${len}) beyond ${buf.byteLength} bytes`,
        );
      }

      return buf.subarray(off, off + len);
    }

    const response = await this.request(name, {
      Range: `bytes=${off}-${off + len - 1}`,
    });

    // A host that answered 206 once can still answer 200 later (a CDN edge
    // without range support, a config change mid-session). Degrade to
    // whole-file for the object rather than failing the read.
    if (response.status === 200) {
      const bytes = new Uint8Array(await response.arrayBuffer());
      this.stats.bytesFetched += bytes.byteLength;
      this.buffers.set(name, bytes);
      this.ranged.delete(name);
      this.stats.mode = "whole-file";

      return this.readAt(name, off, len);
    }

    if (response.status !== 206) {
      throw new Error(`${name}: HTTP ${response.status} for ranged read`);
    }

    const bytes = new Uint8Array(await response.arrayBuffer());
    this.stats.bytesFetched += bytes.byteLength;

    if (bytes.byteLength !== len) {
      throw new Error(
        `${name}: asked for ${len} bytes at ${off}, got ${bytes.byteLength}`,
      );
    }

    return bytes;
  }

  async whole(name) {
    if (this.buffers.has(name)) {
      return this.buffers.get(name);
    }

    const response = await this.request(name, {});
    if (!response.ok) {
      throw new Error(`${name}: HTTP ${response.status}`);
    }

    const bytes = new Uint8Array(await response.arrayBuffer());
    this.stats.bytesFetched += bytes.byteLength;
    this.buffers.set(name, bytes);

    return bytes;
  }

  async request(name, headers, method = "GET") {
    this.stats.requests += 1;

    const response = await this.fetch(this.url(name), { method, headers });

    return response;
  }
}

// parseContentRange pulls the total size out of `bytes 0-0/12345`. A `*` total
// (allowed by RFC 9110) is useless here, so it parses to null and the caller
// treats the host as not really supporting ranges.
export function parseContentRange(value) {
  const match = /^bytes\s+\d+-\d+\/(\d+)$/.exec(value ?? "");

  return match ? Number(match[1]) : null;
}
