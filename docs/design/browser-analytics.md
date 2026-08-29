# Bounded browser analytics

Issue [#48](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/48)
asks for useful point-in-time slicing before retained generations make global
timelines truthful. This design adds filtered facets to the resident browser
engine without changing the corpus or crawl.

## Decision

An opt-in `include_facets` search computes exact row counts for five dimensions
during the existing match scan:

- normalized employment type;
- structured or existing heuristic workplace type;
- annual, other, or undisclosed compensation;
- mutually exclusive board-posted age buckets;
- mutually exclusive corpus first-seen age buckets.

Every dimension has fixed cardinality and an `unknown` or `undisclosed` bucket.
The result values map directly to existing employment, workplace, compensation,
and posted-since filters. First-seen is overview-only because it is corpus
observation time, not an employer's publication claim.

The response says `count_unit: "rows"`. These are corpus row counts, including
historical rows when `include_closed` is selected. They are not deduplicated
open-listing counts. The manifest's `open` remains the distinct dedupe-key union
for the whole generation.

## Why not a summary artifact

The current corpus is latest-only. A new point-in-time artifact could make the
unfiltered overview cheap, but it could not answer a filtered drill-down without
either loading another multidimensional index or scanning the resident rows.
The existing scan already visits every row to produce an exact match count, and
all selected fields are already loaded for filters or cards. Counting alongside
that scan therefore adds:

- zero corpus bytes;
- zero column requests;
- zero persistent row memory;
- no generation, format, digest, or migration change.

Company and location top lists are deliberately excluded. Their cardinality is
not bounded by the schema, so exact filtered top-K counts need memory proportional
to distinct values or a separately measured index. Source platform is excluded
from this slice because it has no shared query filter yet.

## Compatibility and lifecycle truth

The API is additive. `include_facets` defaults false and `facets` is omitted in
that case. Corpus format version 1 and minimum reader version 1 are unchanged.
Missing optional columns already decode to zero values and therefore count as
unknown. Unexpected enum strings and future timestamps also count as unknown,
so malformed input cannot create new buckets or grow memory.

Posted age uses `PostedAt`, which is the board's claim. First-seen age uses the
corpus's immutable observation. Neither is presented as a historical market
timeline, and closure intervals remain untouched.

## Browser work and cancellation

Facets run in the existing worker. Superseded interactive searches now send a
cancellation token through `EngineClient`, the worker, and the Wasm bridge. The
Wasm scan yields to the worker event loop every 32,768 rows so cancel messages
can run; the Go engine checks its context every 1,024 rows. Saved-search rollups
remain independent and are not cancelled by interactive typing.

## Budgets

The measured baselines and results are in
[`../measurements/2026-08-29-browser-analytics.md`](../measurements/2026-08-29-browser-analytics.md).
The initial budgets are:

| Resource | Budget |
| --- | ---: |
| Corpus artifact and projected-column bytes | no increase |
| Wasm linear memory after load | no measurable increase |
| Per-row facet allocations | zero |
| Facet cardinality | 22 counters total |
| Gzipped Wasm payload | at most 1,180,000 bytes, enforced by `web/build.sh` |
| Production-scale faceted scan in the measurement orb | at most 1.3 seconds |
| Cancellation observation | by the next 32,768-row boundary and worker task turn |

Wall-time budgets are pinned to the measurement orb and harness, not claimed as
universal browser latency. A real-device budget needs browser telemetry from a
repeatable local trace, not tracking in the product.
