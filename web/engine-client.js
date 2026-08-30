// engine-client.js — the page's async handle on the worker-hosted engine.
//
// The surface mirrors the wasm bridge (open, load, search, detail) as promises, plus
// a callback for the progress stats the worker volunteers during load. Every call is
// request/response matched by id; the worker crashing rejects everything
// in flight rather than leaving the page waiting forever.

export class EngineClient {
  constructor() {
    this.worker = new Worker("worker.js", { type: "module" });
    this.pending = new Map();
    this.nextID = 1;
    this.onProgress = null;

    this.worker.onmessage = (event) => {
      const message = event.data;

      if (message.op === "progress") {
        this.onProgress?.(message);
        return;
      }

      const entry = this.pending.get(message.id);
      if (!entry) return;
      this.pending.delete(message.id);

      if (message.ok) entry.resolve(message.value);
      else entry.reject(new Error(message.error));
    };

    this.worker.onerror = (event) => {
      const err = new Error(event.message || "the search engine worker failed");
      for (const entry of this.pending.values()) entry.reject(err);
      this.pending.clear();
    };
  }

  call(op, args, { signal } = {}) {
    return new Promise((resolve, reject) => {
      const id = this.nextID++;
      const abort = () => {
        this.pending.delete(id);
        if (op === "search" || op === "detail") {
          this.worker.postMessage({ op: "cancel", args: { token: id } });
        } else {
          this.worker.terminate();
          for (const entry of this.pending.values()) {
            entry.reject(new DOMException("Snapshot preparation timed out", "TimeoutError"));
          }
          this.pending.clear();
        }
        reject(new DOMException(op === "search" ? "The search was cancelled" : "Snapshot preparation timed out", op === "search" ? "AbortError" : "TimeoutError"));
      };

      if (signal?.aborted) {
        abort();
        return;
      }

      const settle = (fn) => (value) => {
        signal?.removeEventListener("abort", abort);
        fn(value);
      };
      this.pending.set(id, { resolve: settle(resolve), reject: settle(reject) });
      signal?.addEventListener("abort", abort, { once: true });
      this.worker.postMessage({ id, op, args: { ...args, token: id } });
    });
  }

  open(corpusURL, options) {
    return this.call("open", { corpusURL }, options);
  }

  load(options) {
    return this.call("load", {}, options);
  }

  search(request, options) {
    return this.call("search", { request }, options);
  }

  detail(url, options) {
    return this.call("detail", { url }, options);
  }
}
