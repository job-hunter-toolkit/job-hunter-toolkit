// Unit coverage for the worker protocol's cancellation contract.

let worker;
const workers = [];

class FakeWorker {
  constructor() {
    worker = this;
    workers.push(this);
    this.messages = [];
  }

  postMessage(message) {
    this.messages.push(message);
  }

  terminate() {
    this.terminated = true;
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

const failedLoad = client.load();
const failedLoadRequest = worker.messages.at(-1);
worker.onmessage({ data: { id: failedLoadRequest.id, ok: false, error: "bad column", phase: "decode", retryable: true } });
const failedLoadError = await failedLoad.then(() => null, (err) => err);
if (failedLoadError?.phase !== "decode" || failedLoadError?.retryable !== true) {
  throw new Error(`worker failure lost recovery metadata: ${failedLoadError}`);
}

// Exercise the actual EngineClient.detail -> WebMCP seam. A fake detail
// dependency would miss operation-specific error naming in EngineClient.
const { createWebMCPTools } = await import("../webmcp.js");
const tools = createWebMCPTools({
  getState: () => ({
    phase: "ready",
    summary: {
      generation: 11,
      run_at: "2026-08-29T14:24:56Z",
      rows: 2_005_791,
      open: 1_150_036,
      format_version: 1,
      identity_version: 1,
    },
  }),
  search: (searchRequest, options) => client.search(searchRequest, options),
  detail: (url, options) => client.detail(url, options),
});
const detailController = new AbortController();
const pendingRecord = tools[3].execute({ url: "https://example.com/jobs/1" }, { signal: detailController.signal });
const detailRequest = worker.messages.at(-1);
detailController.abort();
const detailCancellation = worker.messages.at(-1);
const recordResult = await pendingRecord;
if (detailRequest.op !== "detail" || detailCancellation.op !== "cancel" ||
    detailCancellation.args.token !== detailRequest.id || recordResult.error?.code !== "cancelled" ||
    client.pending.size !== 0) {
  throw new Error(`detail cancellation did not cross EngineClient and WebMCP: ${JSON.stringify({ detailRequest, detailCancellation, recordResult })}`);
}

const opening = client.open("https://raw.example/missing/", { signal: AbortSignal.abort() });
const timeout = await opening.then(() => null, (err) => err);
if (timeout?.name !== "TimeoutError" || worker.terminated !== true || client.pending.size !== 0) {
  throw new Error(`open timeout did not terminate worker: ${timeout}`);
}

// Recovery owns a fresh worker. Replacing the failed client leaves exactly one
// active worker and one response listener, rather than retrying against a
// terminated Wasm heap or duplicating an in-flight corpus load.
const recovered = new EngineClient();
if (workers.length !== 2 || workers.filter((candidate) => !candidate.terminated).length !== 1) {
  throw new Error(`recovery did not leave exactly one active worker: ${JSON.stringify(workers)}`);
}
let progressCalls = 0;
recovered.onProgress = () => progressCalls++;
worker.onmessage({ data: { op: "progress", phase: "decode", completed: 1, total: 30 } });
if (progressCalls !== 1) throw new Error("fresh worker did not install exactly one progress listener");
recovered.terminate();
if (workers.filter((candidate) => !candidate.terminated).length !== 0) {
  throw new Error("explicit cleanup left an active worker");
}

console.log("engine client tests passed");
