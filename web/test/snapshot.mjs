import { openSnapshot } from "../snapshot.js";

const missing = "https://raw.example/missing-sha/";
const current = "https://raw.example/corpus/";
const calls = [];
let fallbacks = 0;
const opened = await openSnapshot(
  [missing, current],
  async (base) => {
    calls.push(base);
    if (base === missing) throw new Error("corpus.jhtc: HTTP 404");
    return { generation: 11, content_digest: "verified-by-reader" };
  },
  () => fallbacks++,
);

if (opened.base !== current || opened.summary.generation !== 11 || !opened.recovered) {
  throw new Error(`valid published snapshot was not selected: ${JSON.stringify(opened)}`);
}
if (calls.join(",") !== `${missing},${current}` || fallbacks !== 1) {
  throw new Error(`fallback was not bounded: ${JSON.stringify({ calls, fallbacks })}`);
}

const timeout = new DOMException("timed out", "TimeoutError");
const stopped = await openSnapshot([missing, current], async () => { throw timeout; }).then(
  () => null,
  (err) => err,
);
if (stopped !== timeout) throw new Error("a timed-out worker must not be retried in place");

console.log("snapshot fallback tests passed");
