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
  "config.js",
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
      await Promise.all(
        names
          .filter((name) => name.startsWith("jht-shell-") && name !== SHELL_CACHE)
          .map((name) => caches.delete(name)),
      );
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;

  if (request.method !== "GET") {
    return;
  }

  const url = new URL(request.url);

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
