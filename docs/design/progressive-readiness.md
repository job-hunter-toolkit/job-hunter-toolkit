# Progressive browser readiness

Status: measured design with source-side prototype. [Issue #58](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/58)
tracks browser integration and publication, which still require separate corpus
publication approval. Exact prototype evidence is in
[`../measurements/2026-08-30-bootstrap-prototype.md`](../measurements/2026-08-30-bootstrap-prototype.md).

## Generation 11 critical path

The production app has four real readiness states, but currently exposes only
metadata-loading and fully searchable states:

1. Static HTML paints the complete shell and skeleton cards.
2. `open()` reads manifest, source state, run history, and the table footer.
   Generation, collection time, digest, partial status, and lifecycle totals
   are truthful here.
3. `load()` fetches and decodes every column used by cards, filters, lifecycle
   state, compensation, and global ordering. It interns strings, folds search
   text, computes state, and sorts all 2,005,791 row indexes. Search is not
   valid before this finishes because `Engine` has no partial-row state.
4. The default full scan returns 100 rows, then the main thread creates cards.
   Saved-search rollups run only after that paint. Fixed-cardinality facets are
   opt-in during a query and build no retained index. WebMCP registration is a
   feature-gated dynamic module and does not await the corpus.

PR #70 now reports monotonic network, decode, fold, state, sort, query, and
paint milestones, and supports bounded in-tab recovery. It does not report a
separate duration or allocator sample for every internal sub-operation, so
claims about their individual CPU shares remain assumptions.

## Measurements

Measured 30 August 2026 in this orb against the exact `origin/corpus`
generation 11 files and a secure Portal in Chrome for Testing 152. Browser
numbers are one desktop run, not phone results.

| Boundary | Result |
| --- | ---: |
| Shell first contentful paint | 280 ms |
| DOM content loaded / window load | 305 ms / 321 ms |
| Snapshot metadata visible | Prompt, before corpus projection; exact timestamp was not instrumented |
| Full Node Wasm projection | 16.85 s, 27 reads, 47.0 MiB fetched |
| Full Portal search readiness | 19.3 s |
| Default full-corpus query in Node | 353 ms |
| Title query in Node (`engineer`) | 834 ms |
| Full fixed-cardinality facet scan in Node | 1.01 s |
| Warm Portal basic title tool query | 858 ms |
| Warm Portal same query with facets | 790 ms; one run each, so the apparent improvement is noise |
| Warm visible search, debounce through 100-card paint | 626 ms |
| WebMCP discovery / status invocation | 0.4 ms / 0.8 ms |
| Empty saved-search state read | Below timer resolution per call in a 1,000-call sample |
| Node process after load and queries | 814 MiB RSS, 723.4 MiB Wasm linear memory |

The browser Resource Timing API hid cross-origin range sizes without timing
permission headers. The worker's own measured counter and the exact local
ReaderAt run agree with the user-observed 46.8 MiB and measured 47.0 MiB.
PR #57 remains the authoritative retained-memory measurement: 528 MiB compact
projection and 739 MiB allocator high-water, under the 576 MiB retained and
768 MiB Wasm budgets.

The footer shows why simply moving secondary filters later is not enough:

| Projection | Compressed bytes |
| --- | ---: |
| Title, company, location, URL, platform, first/posted dates, source/state inputs, remote | 42.1 MiB |
| Department, team, employment, workplace, seniority, compensation fields | 1.3 MiB |
| Source state metadata plus footer/open overhead | Remainder of the measured 47.0 MiB |

A truthful newest-first card and basic title search need almost all of the
42.1 MiB core under format v1. URL alone is 16.9 MiB compressed and 164.8 MiB
decoded; title is 14.9 MiB compressed. Prioritizing existing columns would
save only about 1.3 MiB before basic readiness and would preserve the expensive
all-row decode, state computation, and sort. It is not the selected design.

## Selected architecture and prototype

Publish an additive, immutable **bootstrap projection** from the same folded
generation, bound to the manifest content digest:

- snapshot provenance and explicit row-versus-deduplicated count semantics;
- the deterministic first 100 default cards, including lifecycle state and
  every field the existing card renderer uses;
- a schema version, minimum reader version, generation, run time, partial flag,
  and bootstrap digest covered by publication verification;
- no search index, saved-search data, user data, analytics, or new writer.

The browser can verify this small object, paint truthful real jobs, and label
the capability state `default_page_ready` while the unchanged generation 11
engine prepares in the worker. UI controls that require a full scan remain
disabled and say why. WebMCP status reports capability readiness explicitly;
`search_jobs` and `get_job_record` keep returning `not_ready` until the full
engine is ready, so humans and agents cannot disagree.

This is deliberately narrower than claiming basic search readiness early.
Format v1 cannot return detail rows for title-index hits without loading whole
detail columns. Early title search needs a separately measured row-grouped or
covering index design, with stable row locators and lazy verified detail reads.
That design follows the bootstrap projection rather than being smuggled into
it.

The source-side format, generator, validator, atomic local writer, and
bootstrap/full-page parity tests are implemented in `web/bootstrap`,
`web/bootstrapgen`, and `web/engine/bootstrap.go`. Exact generation 11 output
is 81,404 raw bytes / 12,209 deterministic gzip and verifies in 1.72 ms. It is
not published or consumed by the browser yet, so production behavior is
unchanged.

## Compatibility and rollout

- Publication is additive and produced from the existing crawl artifact. It
  must not trigger another crawl.
- Old clients ignore the bootstrap object and retain current behavior.
- New clients fail closed to current behavior if the object is absent,
  malformed, digest-mismatched, from another generation, partial without the
  matching manifest flag, or outside its supported version.
- The service worker must continue bypassing corpus range requests. A stale
  shell must not mix bootstrap cards from one generation with provenance or
  search from another.
- The bootstrap object is bounded and immutable. It is not an unbounded corpus
  export or an offline-search promise.

## Acceptance budgets for the follow-up

These are targets to validate on representative desktop, tablet, and real iOS
hardware, not measurements already achieved:

| Boundary | Desktop target | Phone/tablet target |
| --- | ---: | ---: |
| Shell FCP | at most 500 ms | at most 1.0 s |
| Verified snapshot metadata | at most 1.0 s | at most 2.0 s |
| First useful job paint | at most 2.0 s | at most 4.0 s |
| Basic title search ready | at least 30% faster than the 16.85 s projection baseline, or remain explicitly full-engine-only | Same relative improvement with no termination |
| Full advanced filters/facets ready | no slower than current 19.3 s desktop run | within 75 s existing timeout, with progress |
| Retained / peak Wasm | at most 576 MiB / 768 MiB | at most 576 MiB / 768 MiB |

CI must build a production-row-count fixture, verify bootstrap/full-engine card
parity and generation binding, fail network/decoded/retained-memory regressions,
and test missing/corrupt/stale bootstrap fallback. Browser checks cover
360/768/1280 widths, no horizontal overflow, keyboard order, skeleton-to-card
replacement, accessibility names, and reduced motion.
