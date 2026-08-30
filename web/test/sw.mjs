import { readFileSync } from "node:fs";
import vm from "node:vm";

const handlers = {};
let navigations = 0;
let matchOptions;
const source = readFileSync(new URL("../sw.js", import.meta.url), "utf8");
const context = {
  URL,
  Promise,
  self: {
    location: { origin: "https://site.example" },
    addEventListener: (name, fn) => { handlers[name] = fn; },
    skipWaiting: async () => {},
    clients: {
      claim: async () => {},
      matchAll: async (options) => {
        matchOptions = options;
        return [{ url: "https://site.example/", navigate: async () => { navigations++; } }];
      },
    },
  },
  caches: {
    keys: async () => ["jht-shell-old"],
    delete: async () => true,
  },
};
vm.runInNewContext(source, context);
if (!source.includes('"readiness.js"')) {
  throw new Error("offline shell omitted the readiness and recovery contract");
}

let intercepted = false;
handlers.fetch({
  request: {
    method: "GET",
    url: "https://raw.example/corpus.jhtc.part-000",
    headers: { get: (name) => name === "Range" ? "bytes=10-19" : null },
  },
  respondWith: () => { intercepted = true; },
});
if (intercepted) throw new Error("service worker intercepted a ranged corpus request");

let activation;
handlers.activate({ waitUntil: (promise) => { activation = promise; } });
await activation;
if (navigations !== 1) throw new Error("active stale client was not moved to the new shell");
if (matchOptions?.includeUncontrolled !== true) {
  throw new Error("activation could not see a client controlled by the prior worker");
}

context.caches.keys = async () => ["unrelated-cache"];
activation = undefined;
handlers.activate({ waitUntil: (promise) => { activation = promise; } });
await activation;
if (navigations !== 1) throw new Error("first install caused a duplicate page boot");

console.log("service worker upgrade tests passed");
