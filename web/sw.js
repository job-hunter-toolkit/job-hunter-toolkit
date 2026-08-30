// sw.js. The service worker that makes the page an installable, offline-capable
// app. Two rules, no cleverness:
//
//   1. The APP SHELL (this origin's files, engine.wasm included) is precached
//      at install under a cache name stamped per deploy by web/build.sh. A new
//      deploy changes this file's bytes, which triggers a fresh install and a
//      fresh precache; activate deletes every older cache. Offline, the app
//      always boots.
//
//   2. CORPUS objects (cross-origin) are network-first with a cache fallback,
//      and only complete 200 responses are stored: the Cache API rejects 206
//      partials by spec, and a ranged read must never be served a stale whole
//      body. Offline, the last fully-fetched corpus objects still answer; a
//      never-fetched one fails, and the page says so rather than guessing.
//
// The build stamp below is substituted by web/build.sh; in a source checkout
// it stays "dev", which is a working cache name, just not a fresh-per-deploy
// one.
const VERSION = "__BUILD__";
const SHELL_CACHE = `jht-shell-${VERSION}`;
const CORPUS_CACHE = "jht-corpus-v1";

const SHELL = [
  "./",
  "index.html",
  "style.css",
  "app.js",
  "card.js",
  "config.js",
  "snapshot.js",
  "corpus-store.js",
  "freshness.js",
  "rollup.js",
  "worker.js",
  "engine-client.js",
  "wasm_exec.js",
  "engine.wasm",
  "manifest.webmanifest",
  "icon.svg",
  "icon-maskable.svg",
  "apple-touch-icon.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.addAll(SHELL))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys();
      const oldShells = names
        .filter((name) => name.startsWith("jht-shell-") && name !== SHELL_CACHE);
      const staleClients = oldShells.length > 0
        ? await self.clients.matchAll({ type: "window", includeUncontrolled: true })
        : [];
      await Promise.all(oldShells.map((name) => caches.delete(name)));
      await self.clients.claim();

      // A page controlled by the prior worker is still running the old
      // JavaScript until navigation. A first-time visitor was uncontrolled and
      // must not pay for a duplicate boot. Reload it once so a stale installed
      // PWA cannot keep requesting a corpus layout the new shell can replace.
      await Promise.all(staleClients.map((client) => client.navigate(client.url)));
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;

  if (request.method !== "GET") {
    return;
  }

  const url = new URL(request.url);

  // Some Safari versions drop Range while a request passes through a service
  // worker, turning a few-megabyte column read into a 38-90 MiB whole-part
  // download. Let the browser networking stack send ranged corpus reads
  // directly. There is no useful cached 206 response to give up: CacheStorage
  // rejects partial responses.
  if (request.headers.get("Range")) {
    return;
  }

  if (url.origin === self.location.origin) {
    event.respondWith(shellFirst(request));
    return;
  }

  if (url.protocol === "https:" || url.protocol === "http:") {
    event.respondWith(corpusNetworkFirst(request));
  }
});

// shellFirst serves the precached shell, falling back to the network for
// anything same-origin the precache does not know (and caching what it finds).
// A navigation that misses everything gets index.html, so a deep link works
// offline too.
async function shellFirst(request) {
  const cached = await caches.match(request, { ignoreSearch: true });
  if (cached) {
    return cached;
  }

  try {
    const response = await fetch(request);
    if (response.ok && response.status === 200) {
      const cache = await caches.open(SHELL_CACHE);
      cache.put(request, response.clone());
    }

    return response;
  } catch (err) {
    if (request.mode === "navigate") {
      const index = await caches.match("index.html");
      if (index) {
        return index;
      }
    }

    throw err;
  }
}

// corpusNetworkFirst prefers fresh data and keeps the last complete copy for
// offline. Only status-200, non-opaque responses are stored: 206 partials are
// rejected by the Cache API, and opaque responses cannot be validated.
async function corpusNetworkFirst(request) {
  try {
    const response = await fetch(request);

    if (response.status === 200 && response.type !== "opaque" && !request.headers.get("Range")) {
      const cache = await caches.open(CORPUS_CACHE);
      cache.put(request, response.clone());
    }

    return response;
  } catch (err) {
    const cached = await caches.match(request, { cacheName: CORPUS_CACHE });
    if (cached) {
      return cached;
    }

    throw err;
  }
}
