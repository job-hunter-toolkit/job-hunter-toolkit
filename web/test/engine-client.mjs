// Unit coverage for the worker protocol's cancellation contract.

let worker;

class FakeWorker {
  constructor() {
    worker = this;
    this.messages = [];
  }

  postMessage(message) {
    this.messages.push(message);
  }
}

globalThis.Worker = FakeWorker;

const { EngineClient } = await import("../engine-client.js");
const client = new EngineClient();

const controller = new AbortController();
const pending = client.search({ titles: ["engineer"] }, { signal: controller.signal });
const request = worker.messages[0];

if (request.op !== "search" || request.args.token !== request.id) {
  throw new Error(`search token was not tied to request id: ${JSON.stringify(request)}`);
}

controller.abort();
const cancellation = worker.messages[1];
if (cancellation.op !== "cancel" || cancellation.args.token !== request.id) {
  throw new Error(`wrong cancellation message: ${JSON.stringify(cancellation)}`);
}

const error = await pending.then(
  () => null,
  (err) => err,
);
if (error?.name !== "AbortError" || client.pending.size !== 0) {
  throw new Error(`cancel did not reject and clean up: ${error}`);
}

// A late worker response to cancelled work is deliberately ignored.
worker.onmessage({ data: { id: request.id, ok: true, value: { matched: 1 } } });

console.log("engine client tests passed");
