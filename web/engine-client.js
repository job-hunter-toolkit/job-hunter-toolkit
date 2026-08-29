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

  call(op, args) {
    return new Promise((resolve, reject) => {
      const id = this.nextID++;
      this.pending.set(id, { resolve, reject });
      this.worker.postMessage({ id, op, args });
    });
  }

  open(corpusURL) {
    return this.call("open", { corpusURL });
  }

  load() {
    return this.call("load");
  }

  search(request) {
    return this.call("search", { request });
  }
}
