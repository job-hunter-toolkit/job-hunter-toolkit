# Generation-relative posting date trust

## Decision

A source `posted_at` is anomalous when it is later than the immutable corpus
generation `run_at` by more than 15 minutes. The policy never reads the viewer's
clock.

The reader preserves and returns the original `posted_at`. It also returns:

- `date_anomaly: "future"` for an anomalous source value;
- `effective_sort_basis`, either `posted_at` or `first_seen`;
- `effective_sort_at`, the timestamp used within that basis.

Trusted source dates sort first, newest first. Missing and anomalous source
dates form a fallback group sorted by `first_seen` descending. Both groups then
use company, title, and deterministic table row index as the same total-order
ties. This group boundary matters: `first_seen = run_at` must not let a newly
observed bad source date outrank a plausible source date from the same run.

Posted-since queries and posted-age facets use the same effective date. An
anomalous date cannot satisfy recency and has unknown posted age. A trusted
date inside the skew allowance is clamped to zero age for the facet only. The
source value is never rewritten.

## Generation 11 measurement

Measured from immutable corpus commit
`c6c2e2388cbfd5dddad1f0f1312ab17b4b28f34b`, generation 11, 2,005,791 rows,
`run_at = 2026-08-29T14:24:56Z`:

| Horizon after `run_at` | Rows |
| --- | ---: |
| Any positive horizon | 2 |
| More than 1 minute | 2 |
| More than 5 minutes | 2 |
| More than 15 minutes | 2 |
| More than 1 hour | 2 |
| More than 6 hours | 2 |
| More than 24 hours | 2 |
| More than 7 days | 1 |
| More than 30 days | 1 |

The exact rows were:

| Platform | Company | Source date | Horizon | First seen |
| --- | --- | --- | ---: | --- |
| `jibe` | `mountsinai` | `2026-09-01T00:00:00Z` | 57h35m04s | `2026-08-29T14:24:56Z` |
| `avature` | `vanoord` | `2027-01-01T00:00:00Z` | 124d09h35m04s | `2026-08-29T14:24:56Z` |

There were no positive future horizons at or below 15 minutes. The chosen
tolerance therefore quarantines both observed anomalies and changes no
generation 11 classification near its boundary. Fifteen minutes is a bounded
allowance for producer clock skew, not an estimate of source quality.

## Production-size budgets

The final Wasm and the unchanged baseline were run sequentially in the same orb
against the exact generation 11 files. Final versus baseline was 17,471 ms
versus 17,937 ms to load, 370 ms versus 362 ms for the default query, 862 ms
versus 890 ms for a title query, and 1,187 ms versus 1,504 ms for the faceted
overview. The only regression was 2.0% on the default query, within the 3%
budget; the other measured paths were faster. These wall-clock values vary
with host load, so the paired relative result, not a claimed universal speed,
is the useful measurement.

Both builds fetched 47.0 MiB and held 723.4 MiB of Wasm linear memory after
queries. The fixed-width row remains 24 bytes, so the generation 11 resident
model remains below 576 MiB and its peak model remains below 768 MiB. The final
engine is 1,174,076 bytes gzipped, below the existing 1,180,000-byte startup
gate and 0.8% above the 1,165,096-byte baseline. Card normalization has a
deterministic 100-item, 10 ms test and does not add a scan or resident index.

## Presentation and trust boundary

The engine returns a bounded `view` projection with display-only title,
company, location, organization, enum labels, source label, and accessible
name. It deduplicates equal department/team labels and humanizes every
underscore or hyphen-separated enum in one place. Compensation and workplace
machine contracts remain unchanged and stay owned by their dedicated work.

Raw corpus strings remain query inputs. Result strings are bounded before they
cross the Wasm/WebMCP boundary, and the visible renderer uses `textContent`
only. Posting links remain restricted to HTTP(S). WebMCP returns the same item,
view, anomaly, and effective ordering fields as the visible UI. A future
bootstrap projection must serialize this engine output rather than define a
second order or card normalizer.
