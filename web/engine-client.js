// engine-client.js — the page's async handle on the worker-hosted engine.
//
// The surface mirrors the wasm bridge (open, load, search) as promises, plus
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
        this.worker.postMessage({ op: "cancel", args: { token: id } });
        reject(new DOMException("The search was cancelled", "AbortError"));
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

  open(corpusURL) {
    return this.call("open", { corpusURL });
  }

  load() {
    return this.call("load");
  }

  search(request, options) {
    return this.call("search", { request }, options);
  }
}
