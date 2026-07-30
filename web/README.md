# web/ — the corpus-backed website

A static single-page app that searches the latest published corpus generation
entirely client-side. No backend, no proxy, no account: GitHub Pages serves
the code, raw.githubusercontent.com serves the data, and the query engine is
this repository's own Go — `web/engine` over `internal/corpus` and `query` —
compiled to WebAssembly. The browser gets the same query vocabulary and the
same closure semantics as the CLI because it runs the same code.

## Layout

| Path | What it is |
| --- | --- |
| `engine/` | The query core, pure Go, no `syscall/js`. Tested natively (`go test ./web/engine`). |
| `wasm/` | The `js/wasm` entry point: a thin Promise bridge exposing `open`/`load`/`search` on `globalThis.jhtEngine`. |
| `internal/testcorpus/` | Deterministic fixture generation shared by the native tests and the Node harnesses. |
| `fixture/` | CLI wrapper writing that fixture to disk (`go run ./web/fixture -dir <path> [-scale n]`). |
| `index.html`, `style.css`, `app.js` | The DOM layer, kept thin: fetch, wire events, render via `textContent`. |
| `corpus-store.js` | HTTP store for the engine: Range requests where the host honours them, whole-file fallback where it does not. |
| `config.js` | **The single place the corpus location is configured.** |
| `test/` | Node harnesses: `store.mjs` and `config.mjs` (unit, stubbed fetch), `smoke.mjs` (end-to-end against the real wasm), `measure.mjs` (scale measurement). |
| `build.sh` | Assembles the deployable site (default `web/dist`, untracked). |

`.github/workflows/pages.yml` builds and deploys on pushes to master touching
`web/`.

## How data loads

1. `config.js` resolves the corpus branch to a commit SHA via api.github.com
   and pins every fetch to it — an atomic view across a publish that replaces
   the branch. Fallback: the branch-name URL, where a torn read is possible
   but `corpus.Open`'s cross-checks fail loudly rather than plausibly.
2. `corpus-store.js` probes the host with a 1-byte `Range` request. 206 pins
   range mode — the engine then fetches the table footer and exactly the
   columns it decodes, each one contiguous request. A 200 means the host sent
   the whole file; it is kept and served from memory. Degraded, never broken.
3. The engine loads a deliberate projection of the table: the columns queries
   and result cards read, and not the corpus's identity/audit columns
   (id, dedupe_key, closure timestamps, external ids). Measured under Node at
   800,000 rows, that cut load from 12.2 s to 3.4 s and wasm memory from
   1.39 GiB to 652 MiB against the full-row load.

The store only ever sends single byte ranges (`bytes=N-M`). That matters:
raw.githubusercontent.com fails CORS preflights (OPTIONS 403), and single
ranges are CORS-safelisted request headers that trigger none.

## Honesty rules the UI enforces

- The banner names the generation, the crawl instant and its age before any
  posting renders, colour-coded fresh (≤36 h) / aging (≤8 d) / old.
- A `partial` manifest renders a "PARTIAL CRAWL — counts are a floor" warning.
- Searches default to rows currently believed open (states `open` + `stale`);
  closed and lapsed rows appear only behind an explicit checkbox, and every
  result card carries its state badge.
- Counts come from the manifest, which `internal/corpus` computes as a union,
  never a sum.

## Live crawl: the seam

Live querying of CORS-open boards is deliberately not in this pass. The 57%
table in docs/surfaces-and-extensibility.md is curl-measured, and that
document's own first caveat is that the first task of client-side crawling is
re-measuring it from a real browser. The seam is marked in `app.js`
(`LIVE CRAWL SEAM`): a `fetchLive(request)` merging labelled live results
over the snapshot, with the snapshot remaining the fallback for everything.
Any implementation must derive pacing from `internal/httpx`'s limiter table,
not re-invent it.

## Measured numbers (this container, 2026-07-29)

Toolchain go1.26.5; Node 22.22 (V8, the engine Chrome ships). Scale fixture:
`-scale 100000` → 800,000 rows, 300,000 synthetic sources, 31.1 MiB `.jhtc`.

| Number | Value |
| --- | --- |
| `engine.wasm` | 4,093,786 bytes raw, **1,130,762 bytes gzipped** |
| Open (manifest + sources + runs + footer) | 6 reads, 2,489 bytes (fixture scale 1) |
| Load, 800k rows | 3,432 ms, 27 reads; column bytes ≈ 0.15 MiB on this fixture (its dictionary columns are pathologically repetitive; real column bytes will be larger) |
| Search, 800k rows | 117–696 ms per query, predicate-dependent |
| wasm linear memory after load, 800k rows | 652 MiB |

The fixture's `sources.json` (84 MiB for 300k sources) dominates its load
bytes; the real registry is ~8k sources, so the real file is ~100x smaller.

## What is untested, plainly

This container has no browser egress — Chromium cannot load even
example.com — so **nothing here has run in a real browser**. Untested as a
consequence: the DOM layer end to end; `WebAssembly.instantiateStreaming`;
whether a browser really issues the single-range fetches without preflight
(curl evidence twice over, spec on our side, but not the same claim); wasm
memory limits on mobile (652 MiB at 800k synthetic rows is expected to
survive desktop and may not survive mobile Safari — the projection load was
built to shrink exactly this, and streaming/arena loading is the next step if
mobile matters). The wasm engine itself, the store's range logic and the
config resolution are tested under Node (`web/test/`), which exercises
everything except fetch and the DOM.

## Local development

```sh
go run ./web/fixture -dir /tmp/corpus     # a tiny corpus to serve
./web/build.sh                            # assembles web/dist
cp -r /tmp/corpus web/dist/corpus
( cd web/dist && python3 -m http.server 8080 )
# open http://localhost:8080/?corpus=corpus/
```
