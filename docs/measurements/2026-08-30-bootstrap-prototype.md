# Generation 11 progressive-readiness prototype

Measured 30 August 2026 from `origin/master` at `a8102f7` and immutable corpus
commit `c6c2e2388cbfd5dddad1f0f1312ab17b4b28f34b`. The corpus was assembled from
its two published transport parts without crawling or publishing anything.

## Baseline

Generation 11 has 2,005,791 rows and a 132,572,848-byte logical `.jhtc`.
The browser projection reads 27 ranges / 47.0 MiB before it can run an empty
query. A local run of the production Wasm build measured:

| Boundary | Result |
| --- | ---: |
| Wasm load | 14,472 ms |
| Empty default query | 414 ms |
| Title query (`engineer`) | 643 ms |
| Fixed-cardinality facets | 832 ms |
| Process RSS after queries | 742.7 MiB |
| Wasm linear memory after queries | 650.2 MiB |

Production Chrome 152 at a 390 × 844 viewport painted the shell at 260 ms and
100 complete cards at 15,551 ms in one cold run. A same-session desktop reload
painted the shell at 144 ms and completed at 14,435 ms with zero same-origin
resource transfer bytes. This confirms that a warm shell and HTTP cache do not
remove the all-row decode, fold, lifecycle, sort, query, and paint critical
path. Cross-origin range transfer sizes remain hidden from Resource Timing
without `Timing-Allow-Origin`; the worker's counter and local ReaderAt run are
the byte authority.

The service worker correctly bypasses Range requests and caches only complete,
non-opaque 200 responses. It therefore provides an offline shell, not offline
full search. No persistent derived index exists. Storage refusal, eviction,
quota, private-mode persistence, and data-saver policy do not change current
search correctness because current search does not depend on persisted browser
storage.

## Implemented source-side slice

`go run ./web/bootstrapgen -corpus <dir> -output <path>` now builds a compact
default-page document from the same loaded engine used by the browser. It:

- carries generation, content digest, format/identity versions, run time,
  partial status, row count, exact request semantics, count units, and the
  complete first 100 open-or-stale cards;
- pins lifecycle evaluation explicitly, defaulting to immutable `run_at` for
  deterministic publication bytes;
- binds each card to page position plus source table row under the corpus
  content digest;
- has an independent SHA-256 payload digest and a hard 256 KiB raw limit;
- rejects malformed, truncated, oversized, partial-page, duplicate-row,
  wrong-version, cross-generation, wrong-digest, wrong-format, wrong-identity,
  wrong-row-count, and partial-status mismatches;
- validates before atomically replacing a local output, leaving an existing
  committed file untouched on failure;
- preserves additive-field compatibility for old readers and keeps hostile
  instruction-shaped corpus strings as inert data.

The native-only `DefaultBootstrapPage` exposes source row bindings without
adding a Wasm operation, worker, resident index, or browser protocol. Its test
compares every card, count, lifecycle total, and order against the complete
empty search. Search and detail remain full-engine-only.

## Exact generation 11 result

| Artifact fact | Result |
| --- | ---: |
| Cards | 100 |
| Raw JSON | 81,404 bytes |
| gzip -9 (`-n`, no filename/timestamp header) | 12,209 bytes |
| Payload digest | `3b877a5193e3dc43ce5d9ced02b9db5aa6e3e3322761c8ac880001df2fb0b4d4` |
| Generator wall time / peak RSS | 6.21 s / 630.8 MiB |
| Verification, 1,000 iterations | 1.720 ms each |
| Default matching rows at `run_at` | 1,366,326 (1,150,036 open + 216,290 stale) |

The artifact is 0.17% of the current 47.0 MiB browser projection raw transfer,
or 0.025% at measured gzip size. That makes a verified first-card path within
the 2 s desktop / 4 s phone target plausible, but it is not a measured browser
result because no object was published and no browser consumer was added. The
current production first useful paint remains the complete paint above.

## Architecture disposition

The additive bootstrap remains the highest-value first step. Staging existing
secondary columns saves only about 1.3 MiB and leaves URL/title decode, state,
and the two-million-row sort. A basic-search index needs compact stable row
locators and lazy verified detail reads before it can claim complete search.
Digest-keyed IndexedDB/OPFS derived indexes may improve repeat visits but add
quota, eviction, crash promotion, private-mode, and Safari behavior without
improving a first visit. Delta transport helps updates only after a verified
base exists. Format v2, partitioning, streaming decode, snapshots, and retained
generations should therefore wait for their own measured prototypes rather than
block the bounded first page.

CRDTs are rejected: corpus generations are immutable nightly snapshots from one
writer, and readiness caches contain no concurrently editable state. There is
nothing to reconcile.

## Rollout boundary and remaining evidence

No bootstrap object, manifest pointer, workflow, service-worker behavior, or
browser loading path changed. Publication requires explicit approval. A later
rollout must publish the object from the existing folded artifact, fetch
foreground metadata first, verify all bindings before paint, label cards as a
verified default page evaluated at the stated instant, and fall back to the
unchanged complete path on every failure. It must not put 206 responses in
Cache Storage or expose search/detail before complete readiness.

Browser integration still needs one-worker/download and cancellation tests,
missing/corrupt/cross-generation fallback, cold/warm/offline/storage-refusal
coverage, and DOM/keyboard/screen-reader/reduced-motion/no-overflow checks at
360/390/768/1280. No physical iOS device was available in this orb, so Safari
WebContent headroom and real iOS first-paint timing remain unverified.
