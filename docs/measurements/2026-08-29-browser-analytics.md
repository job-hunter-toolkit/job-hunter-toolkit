# Browser analytics measurement, 2026-08-29

## Inputs

- Baseline: `master` at `3fc6167`.
- Proposed branch: `feat/issue-48-browser-analytics`.
- Representative deterministic fixture: 100,000 rows.
- Production generation 10 at `b4761b8`, downloaded by immutable SHA:
  2,005,791 rows, 9,888 sources, 125.8 MiB logical `.jhtc`.
- Harness: `web/test/measure.mjs` under Node/V8 in the same Amp orb.

Generation 10's manifest reports 1,548,484 distinct believed-open listings,
while the engine's default lifecycle selection contains 1,764,781 rows. This is
the observed reason analytics responses explicitly call their unit rows.

## Results

### Representative fixture

| Metric | Baseline | Proposed |
| --- | ---: | ---: |
| Engine Wasm, raw | 4,099,996 B | 4,186,187 B |
| Engine Wasm, gzip -9 | 1,132,599 B | 1,154,387 B |
| Load wall | 619 ms | 627 ms |
| Projected bytes fetched | 10.5 MiB | 10.5 MiB |
| Wasm linear memory | 102.0 MiB | 102.0 MiB |
| Match-all scan | 9.5 ms | 15.9 ms |
| Faceted match-all scan | not available | 45.1 ms |

The gzip increase is 1.92%. Load variance is 1.3% in this single pair and no
new load path, corpus byte, or resident field exists. The unchanged fetched and
linear-memory values are the stronger resource evidence.

### Production generation 10

| Metric | Baseline | Proposed |
| --- | ---: | ---: |
| Load wall | 32.1 s | 23.9 s |
| Projected bytes fetched | 46.9 MiB | 46.9 MiB |
| Wasm linear memory | 1,819.7 MiB | 1,820.0 MiB |
| Match-all scan | 397 ms | 491 ms |
| Faceted match-all scan | not available | 1,195 ms |

The load improvement is treated as run variance, not a claimed optimization.
This slice changes no load code or loaded columns. The production result is
inside the 1.3-second faceted-scan budget, with no additional corpus transfer or
resident Wasm memory.

## Cancellation and correctness

- The native test cancels at the first 32,768-row yield and observes
  `context.Canceled`.
- A direct Wasm probe against generation 10 requested a faceted full-history
  scan, sent cancellation after 10 ms, and observed `context canceled` after
  54 ms.
- The deterministic fixture verifies every facet sums to matched rows under the
  default lifecycle selection, filtered searches, and full history.
- Malformed enum values and future dates remain in fixed unknown buckets.
- The compiled Wasm smoke test opens the unchanged format-1 fixture and verifies
  filtered facets end to end.
