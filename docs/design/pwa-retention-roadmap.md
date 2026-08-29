# PWA, retention, and data evolution roadmap

Measured 2026-08-29 for [issue #48](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/48).
This roadmap owns return value, local state, offline truthfulness, and the data
contracts needed for history. Responsive information architecture and query
index performance remain separate workstreams.

## 1. What exists

### 1.1 Production and publication

Production generation 10 contains 2,005,791 rows from 9,888 sources. Its
logical `corpus.jhtc` is 131,937,786 bytes, published as 90 MiB and 35.8 MiB
transport parts. The manifest reports 1,548,484 distinct open postings and a
2026-08-29T14:24:56Z run time. These are direct readings from the
[`corpus` branch manifest](https://github.com/job-hunter-toolkit/job-hunter-toolkit/blob/corpus/manifest.json),
not projections.

[`corpus.yml`](../../.github/workflows/corpus.yml) runs daily at 21:00 UTC. It
reuses the successful Track Jobs posting artifact when available, folds once,
verifies the full content digest, then force-replaces a parentless `corpus`
branch commit. This preserves crawl-once/reuse-many and gives a reader one
atomic, SHA-pinned generation. It does not retain addressable corpus
generations. The current generation carries at most 90 compact run summaries;
full postings and crawl diagnostics are Actions artifacts for seven days.

| Fact | Retained today | Can answer |
| --- | --- | --- |
| Current row lifecycle, including `first_seen` and `last_seen` | In latest corpus | Current search and “new since my visit” |
| Last 90 run-level churn summaries | In `runs.ndjson` | Overall publication and churn history |
| Full crawl input | Seven-day Actions artifact | Short rollback and fold diagnosis |
| Prior queryable generation | No | No truthful global timeline or generation-to-generation query comparison |

The format already has the right compatibility primitives. The manifest has
`format_version`, `min_reader_version`, `identity_version`, generation, policy,
and a content digest. Readers ignore additive JSON fields and reject a corpus
whose minimum reader version is newer than they understand. Retention should
extend those contracts, not invent a second corpus identity.

### 1.2 Installability and shell caching

The site has a linked manifest, `standalone` display, start URL, scope, theme
colors, SVG regular and maskable icons, a 180 px Apple touch icon, HTTPS on
GitHub Pages, and a service worker with a fetch handler. Chromium's documented
promotion criteria ask for explicit 192 px and 512 px manifest icons, so the
SVG `sizes: any` entries need a real Chrome installability check before the
project promises install promotion. Safari can install any site and uses the
Apple touch icon; Firefox desktop does not provide manifest-based PWA install.
[MDN documents the platform differences](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable).

The build stamps a new shell cache per commit. Install precaches HTML, CSS,
JavaScript, WASM, manifest, and icons; activate deletes old shell caches. The
same-origin path is cache-first, so the UI shell starts offline after one
successful service-worker install.

The corpus is different. Cross-origin requests are network-first and only
complete, non-opaque 200 responses are cached. Production range reads return
206, which the Cache API cannot store. Consequently:

* metadata and the GitHub API response can be available offline after a prior
  successful open;
* the current search projection is normally a set of 206 reads and is not
  available offline;
* the shell can boot offline, but search-readiness offline is not guaranteed;
* the corpus cache has no generation cleanup or explicit byte budget for the
  complete 200 responses it does receive.

The truthful product claim is therefore **offline shell**, not **offline job
search**. Full offline search would require an explicit downloadable object or
an IndexedDB/OPFS range store, quota checks, integrity verification, eviction,
and a user-visible size choice. Browser storage is best-effort by default and
can be evicted under storage pressure; Safari can also proactively evict
script-written data for origins without recent interaction. See
[MDN's quota and eviction summary](https://developer.mozilla.org/en-US/docs/Web/API/Storage_API/Storage_quotas_and_eviction_criteria).

### 1.3 Return state and privacy

Saved searches, previous visit time, and streak live in guarded `localStorage`.
There are at most eight saved searches. The app queries each after the first
results paint and reports matches first observed since the prior visit. No
request, identifier, search, or notification token leaves the browser.
Storage refusal degrades to no memory rather than a broken page.

Before this roadmap the saved-search value was an unversioned array under
`jht.saved.v1`. The foundational slice accompanying this document migrates it
to a fail-closed `{version: 2, searches: [...]}` envelope, bounds and validates
records, and defines data-only export/import functions. It adds no UI,
permission, network call, tracking, or backend. This is the stable boundary for
future local history and user-directed portability.

### 1.4 Background work and notifications

There are four distinct capabilities. They must not be marketed as one:

| Capability | Desktop | Android | iOS/iPadOS | Honest use here |
| --- | --- | --- | --- | --- |
| In-app rollup while open | All current browsers | Yes | Yes | Shipped, reliable when opened |
| One-off Background Sync | Chromium family; not Baseline | Chromium family; Firefox lacks it | Not supported | Retry deferred work, not a daily scheduler |
| Periodic Background Sync | Experimental Chromium-only capability | Installed Chrome PWA, subject to engagement | Not supported | Opportunistic refresh only; browser chooses whether and when |
| Notifications and Web Push | Notifications and push broadly available in current desktop browsers | Push/notifications available, platform details vary | Web Push only for Home Screen web apps on iOS/iPadOS 16.4+ | Requires explicit permission and a push sender/backend |
| App badge | Chrome/Edge desktop; Safari support varies by installed surface | Chrome documents no programmatic badge, launcher may badge unread notifications | Home Screen web apps 16.4+, visible after notification permission | Foreground hint where feature-detected, never a delivery guarantee |

[MDN marks one-off sync](https://developer.mozilla.org/en-US/docs/Web/API/Background_Synchronization_API)
and [periodic sync](https://developer.mozilla.org/en-US/docs/Web/API/Web_Periodic_Background_Synchronization_API)
as limited availability. Chrome requires installation and engagement for
periodic sync, and schedules according to network, power, and browser policy,
not the requested minimum interval. [Chrome's guidance is explicit about that
discretion](https://developer.chrome.com/docs/capabilities/periodic-background-sync).

[WebKit supports standards-based Web Push on iOS and iPadOS 16.4+ only for Home
Screen web apps](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/).
Permission must follow a direct user gesture. Push is not a free client-only
scheduler: a service must retain a subscription and send the event. Major
browsers also require user-visible notifications for push events. Adding push
would therefore add a backend, retained endpoint data, abuse controls, key
rotation, deletion, and operations. That conflicts with the current no-backend
guarantee unless separately justified and approved.

No phase should poll in a hidden tab, imply a daily background execution SLA,
ask notification permission on load, or run a corpus download when data saver,
low storage, or equivalent browser signals say not to. Unsupported APIs degrade
to the open-time rollup.

## 2. Roadmap

### Small: durable local return state

Goal: make opening the app useful without changing corpus, index, or layout
ownership.

1. Ship the versioned saved-search envelope, v1 migration, bounded validation,
   and tested export/import data contract.
2. Add user-triggered export/import UI in the responsive workstream. Export
   only saved query predicates and local labels/timestamps. Import previews
   counts, deduplicates normalized requests, and never uploads a file.
3. Append one bounded point per saved search after a successful open-time
   rollup: `{generation, run_at, observed_at, matched, fresh}`. Keep at most 90
   points and expose clear-history controls. A point is a record of that
   browser's visits, not a claim about days it did not observe.
4. Add manifest and service-worker checks for icon sizes, start URL, shell
   precache completeness, update cleanup, and offline-shell wording.

Exit criteria:

* v1 state migrates once without loss; malformed and future versions fail
  closed; export/import round trips in tests;
* private mode and denied storage still provide normal current search;
* local trend copy says “on your visits” and cannot be mistaken for global
  market history;
* no new network request, account, permission, or identifier.

### Medium: bounded value between publications

Goal: provide useful, opt-in return paths while retaining latest-only corpus
publication.

1. **Reasons to return.** Add 7-day visit-local sparklines after seven observed
   points, generation-aware “changed since last successful open,” and a static
   nightly Atom feed produced from the existing crawl artifact. The feed is a
   pull subscription and requires no browser permission or user registry.
2. **Current-generation comparison and downloads.** Compare saved searches
   within one generation across current dimensions. Export a user-requested,
   bounded CSV/NDJSON slice with an explicit field allowlist and generation,
   run time, filter, lifecycle, and truncation metadata. Never export hidden
   corpus identity/audit columns by accident.
3. **Offline by selection.** Measure projected bytes and heap first. Offer a
   clearly sized saved-search snapshot or precomputed result slice, not an
   automatic 132 MB corpus download. Use storage estimates, verify the corpus
   digest/generation, keep one committed version plus one staging version, and
   expose remove/refresh controls. “Available offline” appears only after an
   executed offline read test succeeds.
4. **Opportunistic refresh.** Feature-detect periodic sync only after install
   and explicit opt-in. At most request daily metadata or a small saved slice;
   check connection and storage signals. Foreground open remains the source of
   truth. One-off sync is only a retry for an interrupted user request.
5. **Truthful alerts.** Prefer in-app rollup, Atom, and feature-detected badge.
   Do not implement push without a separate backend/privacy/abuse decision.

Exit criteria:

* cold, warm, offline, low-quota, and upgrade paths run in deterministic tests
  plus Chrome, Android Chrome, and a real iOS Home Screen app;
* every background control states that timing is browser-controlled and shows
  its last successful refresh;
* network, storage, CPU, and battery budgets are measured before rollout;
* local history and downloadable slices are deletable and never transmitted.

### Large: retained facts and historical timelines

Goal: answer market-over-time questions from published evidence rather than
browser visits or latest-row inference.

1. **Retention design first.** Price 7, 30, 90, and 365 days using measured
   generation and aggregate sizes. Specify immutable names, a signed/indexed
   generation catalog, pruning, rollback, late publication, partial runs, and
   recovery from the current replaced orphan branch. Keep a single writer.
2. **Materialized summaries.** Derive versioned, content-digested aggregates
   once from each crawl for time × company × source × location × role or
   department × workplace × employment × compensation. Publish only bounded
   dimension/range shards. The browser must not fetch all history.
3. **Truthful comparisons.** Label policy changes, source failures, partial
   crawls, closure delays, source additions/removals, enrichment coverage, and
   denominator changes. A generation comparison pins both content digests and
   refuses incompatible identity or policy versions.
4. **Schema evolution.** Add fields where current readers ignore them; bump
   minimum reader version when semantics change; bump identity version only
   when ID derivation changes. Test old-reader refusal, mixed-generation
   rejection, migration, rollback, digest coverage, and pruning.
5. **Coverage evolution.** Publish source/company coverage facts from registry
   and manifests: registered, attempted, qualifying, stale, failed, retired,
   and postings represented. New ATS breadth follows measured candidate demand,
   browser/API evidence, pacing, and fixture ownership. It is not coupled to a
   chart request. Map layers wait for crawl-time geocoding with measured
   coverage and provenance.

Exit criteria:

* a reviewed storage and Actions cost model sets the retention window;
* retained objects are immutable, digest-addressed, prunable, and recoverable;
* a deterministic fixture proves timeline answers across policy, schema,
  source-coverage, partial-run, and rollback boundaries;
* one crawl artifact still feeds corpus, summaries, downloads, and feeds;
* no tracking or account is introduced. Any push service is an independently
  approved system with deletion and abuse controls, not an implicit phase task.

## 3. Dependency and deduplication map

| Outcome | Current owner or follow-up | Not a new issue |
| --- | --- | --- |
| Responsive 360/768/1280 information architecture | Issue #48 responsive workstream | Layout changes in this roadmap |
| Query/index cold-load performance | Issue #48 analytics-index workstream; exact high-cardinality facets are [issue #50](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/50) | Another index proposal |
| Versioned saved-search storage and migration | Foundation in this roadmap | UI redesign |
| Export/import controls and visit-local trend UI | Issue #48 small phase after responsive ownership settles | A second storage format |
| Current query download | Issue #48 Phase 1 already specifies it | Historical retention |
| Offline selected slices and opportunistic refresh | [Issue #52](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/52) | “Cache everything” |
| Notification/platform decision and Atom feed | [Issue #53](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/53) | Push implementation |
| Retained generations, historical summaries, migrations | [Issue #51](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/51) | Inferring history from latest rows |
| Source/company coverage governance | [Issue #54](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/54) | One issue per unmeasured employer |
| CLI crawl cache | Existing issue #1 | Browser cache work |

This ordering keeps the no-backend, no-tracking, pull-based product intact. It
earns retention with local value first, then measures bounded publication, and
only then pays for global history.
