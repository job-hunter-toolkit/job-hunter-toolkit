# CLAUDE.md

The job hunter's toolkit: a Go CLI that crawls ~10,000 company job boards
across 22 ATS platforms, a nightly corpus pipeline, and a no-backend PWA whose
query engine is the same Go code compiled to WebAssembly.

## Commands

```console
$ go build ./... && go test ./...        # hermetic; no network needed
$ node web/test/config.mjs && node web/test/snapshot.mjs && node web/test/readiness.mjs && node web/test/store.mjs && node web/test/engine-client.mjs && node web/test/sw.mjs && node web/test/rollup.mjs && node web/test/freshness.mjs && node web/test/card.mjs && node web/test/query-state.mjs && node web/test/webmcp.mjs
$ ./web/build.sh && node web/test/smoke.mjs web/dist <corpus-dir>
$ go run ./web/fixture -dir <dir>        # deterministic test corpus for the site
```

## Conventions

- **Docs**: mermaid diagrams in markdown; ASCII art only in code and
  terminals. No em-dashes in prose (docs, UI copy, commit messages). No
  emojis in the product. "Less is more": charts, diagrams, and the code do
  the talking. Design docs argue from measurements and label unmeasured
  claims as assumptions; `docs/design/README.md` is the index.
- **Web**: `web/dist` is build output and stays untracked; pages.yml
  assembles it. All posting data reaches the DOM through textContent, never
  innerHTML. Posting links and the `?corpus=` override are scheme-checked to
  http(s).
- **Browser lessons paid for in outages, do not relearn them**: native
  `fetch` must be called with `globalThis` as its receiver (store it bound,
  never as a bare method); cross-origin response headers are invisible unless
  CORS-safelisted, so size objects via HEAD Content-Length, never
  Content-Range; the Cache API rejects 206 partials; a UA's `[hidden]` rule
  loses to any explicit `display`, so the stylesheet pins `[hidden]` with
  `!important`.
- **Chart** (`jobs_record.gnuplot`): y-tics derive from the data via stats;
  never hardcode tick increments; stats runs before `set xdata time` and all
  stats vars are read through `exists()`.
- **Crawling ethics**: service-aware pacing is never bypassed; more
  parallelism or IPs are not permission to raise pressure on a shared
  backend.

## Product vision: the companion app

The web app should be something a person opens every morning and gets real
value from, with the care of a Linear-grade product. Everything user-facing
stays client-side and pull-based, so spam is structurally impossible and the
nightly pipeline stays the only writer. Sequenced:

1. **PWA + saved searches + open-time rollup** (shipped): installable,
   offline-capable shell; saved searches in localStorage; a greeting card on
   open summarizing new postings per saved search since the last visit, with
   a day streak. Data budget: revisits cost a manifest check unless a new
   generation shipped.
2. **Notifications, honestly scoped**: no server means no web push. In-app
   rollup, installed-app badging and periodic background sync where
   supported, plus a public Atom feed of the nightly rollup published by the
   corpus workflow for RSS readers.
3. **Live refresh of saved searches**: the CORS-open majority of boards can
   be queried straight from the browser for just the companies matching
   saved searches; the seam is marked in web/app.js. Requires re-measuring
   the CORS table from a real browser first.
4. **Trend lines from retained visits**: each rollup run appends
   {date, matched} per saved search to localStorage; after a week the rollup
   chips carry 7-day sparklines of the user's own searches, and longer
   retention earns richer market-trend views. Client-side accumulation only;
   corpus generations plus the jobs_record series are the server-side data
   if a global trend surface is ever wanted.
5. **Map and visual layers**: needs a geocoding pass in internal/enrich
   (offline gazetteer, applied at crawl time, shipped as corpus columns)
   before any map UI is worth building.

Constraints that outrank features: configurable, quiet by default, works
offline, cheap on data and battery, no accounts, no tracking.

Taste bar: modern professional tool, never tacky. No gradient text, no
emojis, no gimmick animation; motion is brief, purposeful, and collapses
under prefers-reduced-motion; haptics fire only on user gestures (Chromium
blocks gestureless vibrate, and it was right to). Whimsy must be factual,
like the loading line naming real employers as their postings stream in.
