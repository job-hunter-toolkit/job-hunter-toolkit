# The storage engine

`docs/architecture-roadmap.md` Phase 3 says "Start with SQLite through a pure-Go
driver." This document argues, with measurements, that it should not — and that
the right engine for this corpus is not a database at all.

Everything below was measured on 2026-07-28 in a 4-vCPU / 16 GB container with
Go 1.26.5, against 780,481 synthetic postings shaped by the platform
distribution in `docs/measurements/2026-07-28-crawl.md`. The page cache was
dropped (`/proc/sys/vm/drop_caches`) before every read measurement. Prototypes
are in the scratchpad, not the repo; nothing here is extrapolated, and the
places where I did not measure say so.

## Recommendation

**Store the corpus in a purpose-built columnar file (`.jhtc`) written with
nothing but the standard library, and put no database in the default binary.**

Behind `storage.Backend` (§4), ship three implementations: `memory`, a
read-only `ndjson` reader over the snapshots that already exist, and `corpus`
over `.jhtc`. Keep a Parquet **exporter** out of the default binary, as a
separate `main` package, so the analytical ecosystem is one command away
without the crawler linking it.

The headline reason is that the format is not the hard part. A ~300-line
prototype using `compress/flate`, `encoding/binary` and nothing else produced a
**smaller file, faster scans, fewer bytes over the wire and a faster load than
`parquet-go`, on both native and `js/wasm`, while adding zero modules**:

| | `.jhtc` (stdlib) | Parquet (`parquet-go`) | SQLite (`ncruces`) |
| --- | ---: | ---: | ---: |
| new modules linked | **0** | 10 | 4 |
| binary cost over hello-world | **+0.26 MiB** | +6.64 MiB | +12.15 MiB |
| corpus file | **19.0 MiB** | 25.0 MiB | 398.9 MiB |
| count-by-platform, cold, native | **56 ms** | 91 ms | 171–257 ms |
| count-by-platform, cold, `js/wasm` | **202 ms** | 302 ms | 5.00 s |
| median-open-days, cold, `js/wasm` | **112 ms** ¹ | 275 ms | 67.07 s |
| bytes read to answer count-by-platform | **2.11 MiB / 2 reads** | 2.35 MiB / 58 reads | 98.59 MiB / 12,619 reads |
| deterministic across two runs | yes | yes | yes ² |

¹ after a 1.06 s resident load, which the surfaces that ask this question do once.
² incidentally, and only for a fresh sequential build. See §6.

The runner-up is Parquet, not SQLite. If a reviewer's tolerance for a bespoke
format is low, take Parquet and pay the 10 modules; every conclusion in §3 about
SQLite still stands. The decision is also cheap to reverse — see §8.

## 1. The portability gate eliminates most candidates before performance matters

Constraint 2 is that `GOOS=js GOARCH=wasm` and `GOOS=wasip1 GOARCH=wasm` both
build today, unmodified, and must keep building. That is a build-time fact, so I
tested it first rather than benchmarking things that cannot ship.

`CGO_ENABLED=0 GOOS=… GOARCH=… go build ./...`, Go 1.26.5:

| candidate | version | linux/amd64 | linux/arm64 | darwin/arm64 | windows/amd64 | js/wasm | wasip1/wasm |
| --- | --- | :-: | :-: | :-: | :-: | :-: | :-: |
| `modernc.org/sqlite` | v1.54.0 | ✓ | ✓ | ✓ | ✓ | **✗** | **✗** |
| `go.etcd.io/bbolt` | v1.5.0 | ✓ | ✓ | ✓ | ✓ | **✗** | **✗** |
| `github.com/cockroachdb/pebble/v2` | v2.1.6 | ✓ | – | – | – | **✗** | **✗** |
| `github.com/parquet-go/parquet-go` | v0.30.1 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `github.com/ncruces/go-sqlite3` | v0.35.2 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| standard library only | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

The failures, verbatim, because "check whether it builds, do not assume" was
part of the assignment:

- `modernc.org/sqlite` →
  `modernc.org/libc/errno: build constraints exclude all Go files` (also
  `limits`, `pthread`, `signal`, `stdio`). It is pure Go, but `modernc.org/libc`
  is a POSIX emulation with per-GOOS packages and there is no `js` or `wasip1`
  one. Its module graph is 26 entries (`go list -m all`).
- `go.etcd.io/bbolt` →
  `internal/common/unsafe.go:26:12: undefined array length MaxAllocSize`.
  bbolt is mmap-based; there is no mmap on `js`.
- `pebble` → `internal/rawalloc/rawalloc.go:23:12: undefined array length
  maxArrayLen`, plus six `vfs` errors including
  `file_lock_generic.go:16:7: undefined: defFS`. Its module graph is 135 entries.

`mattn/go-sqlite3` and DuckDB are excluded a priori by invariant: "the default
binary stays portable and has no CGO requirement." I did not benchmark them.

Two survivors, plus the option of writing the format.

## 2. What the workload actually is

Before choosing, it is worth being precise about the shape, because it is
unusual and it is what makes a database the wrong tool.

- **~780k rows, flat schema.** `internal.JobPosting` has 15 scalar fields, one
  small embedded struct (`PostingSource`) and one optional flat struct
  (`Compensation`). No repeated groups, no nesting worth the name.
- **One writer, batch, offline.** The corpus is produced by `shard merge` at the
  end of a crawl. There is no concurrent mutation, no transaction contention, no
  point-update workload. `docs/architecture-roadmap.md` already says "SQLite
  should have one writer during artifact merge."
- **The corpus is derived state.** It is rebuildable from the immutable NDJSON
  shard artifacts. Losing it costs a rerun of a merge, not data.
- **Small enough to hold entirely in memory.** Measured: 159.1 MiB of Go heap
  for the seven columns a query surface needs, on both native and `js/wasm`.
- **Predicates are case-insensitive substring matches**
  (`internal.Filter` / `containsAny`), not equality or token search. This matters
  enormously and is the single fact that most damages the SQLite case: a B-tree
  cannot serve `LIKE '%security%'`, and FTS5 answers a *different question*
  (token match, not substring), so switching to it would silently change what
  `--title sec` returns.
- **Aggregates are unbounded scans.** "Count by platform over 90 days", "median
  time a role stays open", "postings that closed this week" all touch every row
  of two or three columns.

A workload that is written once, read whole, filtered by substring and
aggregated by scan is a *columnar file* workload. It is not an OLTP database
workload, and the measurements below are what you would predict from that
sentence.

## 3. The finalists, measured

780,481 rows. Write times include a 1.22 s generator floor common to all of them.

### Write and size

| | write | file | gzip -6 |
| --- | ---: | ---: | ---: |
| NDJSON (today's format) | 3.81 s | 375.9 MiB | 38.2 MiB |
| NDJSON + gzip(BestSpeed) | 5.39 s | 56.1 MiB | – |
| **`.jhtc` (stdlib flate)** | 4.95 s | **19.0 MiB** | – |
| Parquet (zstd + dict) | **2.67 s** | 25.0 MiB | 23.1 MiB |
| SQLite, 4 ordinary indexes | 14.15 s | 316.4 MiB | 66.5 MiB |
| SQLite + covering index | 19.84 s | 398.9 MiB | – |
| SQLite + FTS5 on `title` | +3.90 s | +18.8 MiB | – |

Parquet writes fastest because zstd is faster than `compress/flate`; `.jhtc`
would match it by taking `klauspost/compress` as one module, which I chose not
to do. Five seconds against a 720-second crawl is not worth a dependency.

### Native query latency, cold

| | `.jhtc` | Parquet | SQLite | NDJSON |
| --- | ---: | ---: | ---: | ---: |
| open / init | 2 reads | 0.05 MiB footer, 5 reads | 43 ms | – |
| filtered page 3 (`platform` + two substrings) | 3 ms warm ¹ | 99 ms | **8 ms** ² / 77–125 ms ³ | 4.80 s ⁴ |
| count by platform, 90 days | **56 ms** | 91 ms | 171–257 ms | 4.80 s ⁴ |
| median open days | 48 ms warm ¹ | **145 ms** | 1.91 s | – |
| resident load, 7 columns | **325 ms** | 462 ms | – | – |

¹ after the resident load. ² with a covering index on
`(platform, posted_at DESC, title_fold, location_fold, url)`. ³ with ordinary
indexes. ⁴ one NDJSON pass answers both queries; it cannot answer either faster.

SQLite's 8 ms is real and it is the best single number in the table. It costs an
80 MiB index built for that exact query shape. Add a second query shape and you
add a second index. The resident columnar scan answers *any* predicate in 2–6 ms
with no index at all — measured 6 ms for a different filter
(`workday` + `engineer`, no location term) that no index anticipated.

### `js/wasm` (node 22, same machine)

This is where the candidates separate, and it is a hard constraint rather than a
nice-to-have.

| | `.jhtc` | Parquet | SQLite |
| --- | ---: | ---: | ---: |
| open + `count(*)` | – | – | 866 ms |
| resident load, 7 columns | **1.06 s** | 1.88 s | – |
| filtered page (covering index for SQLite) | 15 ms warm | 451 ms | 823 ms |
| count by platform | **202 ms** | 302 ms | 5.00 s |
| median open days | **112 ms** warm | 275 ms | **67.07 s** |

SQLite really does run under `GOOS=js GOARCH=wasm` — I verified it end to end,
not just that it compiled: 50,000 inserts and a `LIKE` count against a real file
through node's FS shim, `sqlite_version()` 3.53.3, `vfs.SupportsFileLocking`
correctly reporting `false` so the connection needs `nolock=1` and
`SetMaxOpenConns(1)`. It works. It is also 24x slower than the pure-Go columnar
path on the scan and 600x slower on the median, because `ncruces/go-sqlite3`
transpiles SQLite's WASM build to Go via `wasm2go`, and running that Go under
`js/wasm` is a second translation.

### Bytes over the wire

The browser and the daemon-behind-a-CDN both want to answer a query without
downloading the corpus. I measured this with a `ReaderAt` that counts every
read: for SQLite through `vfs/readervfs` (which is exactly what an HTTP-range
VFS would drive), for Parquet through `parquet.OpenFile`, for `.jhtc` through
its own directory.

| | `.jhtc` | Parquet | SQLite |
| --- | ---: | ---: | ---: |
| open | 2 reads, ~4 KiB | 0.05 MiB / 5 reads | – |
| count by platform | **2.11 MiB / 2 reads** | 2.35 MiB / 58 reads | 98.59 MiB / 12,619 reads |
| filtered page, ordinary indexes | – | 1.65 MiB / 145 reads | 57.42 MiB / 7,351 reads |
| filtered page, covering index | – | – | **1.20 MiB / 154 reads** |
| filtered page, FTS5 on title | – | – | 203.47 MiB / 26,045 reads |

Three things worth stating plainly:

- **SQLite is competitive over range requests only when an index covers the
  entire query** — 1.20 MiB, better than Parquet's 1.65 MiB. That is the
  strongest form of the SQLite argument and it is a fair one.
- **FTS5 made it 170x worse.** Each full-text hit becomes a random `rowid`
  lookup into a 400 MiB table, and a random lookup over a range-request VFS is
  an 8 KiB page fetch. This is the general shape of the problem: row storage
  turns selectivity into random I/O.
- **`.jhtc` reads whole columns**, so it is optimal for scans (2 reads) and has
  no story better than "fetch the columns" for a selective point query. At 2 MiB
  a column that is fine; if it stops being fine, the fix is row groups, which is
  a footer change, not a format change.

### Determinism (hard constraint 4)

Two runs over identical input, SHA-256 of the output file: `.jhtc`, Parquet,
NDJSON and SQLite were **all byte-identical**. Two caveats that the raw result
hides:

- SQLite's determinism is incidental. I tested a fresh sequential build from
  sorted input. Freelist state after an update, `VACUUM`, `ANALYZE`'s
  `sqlite_stat1` contents and the library version all affect page layout. Do not
  rely on it.
- Parquet embeds `created_by` =
  `"github.com/parquet-go/parquet-go version 0.30.1(build )"`. No timestamp, so
  it is deterministic — but it changes on every dependency upgrade. If Parquet
  were chosen, `parquet.CreatedBy` must be set to a project-controlled constant.

`.jhtc`'s file-byte determinism depends on `compress/flate` output being stable,
which Go does not promise across releases. §5 removes that dependency.

## 4. The interface

`docs/surfaces-and-extensibility.md` already specifies `Put` / `Query` / `Stats`
with pagination in the interface from the first commit. Two additions: an
explicit `Aggregate`, because the OLAP patterns in the brief cannot be expressed
as `Query` or `Stats` and retrofitting them means changing every implementation
at once; and an opaque cursor rather than an offset.

```go
// Package storage is optional by construction. The crawler produces an iterator
// of postings; a Backend is something a surface attaches, never something the
// crawler requires.
package storage

type Backend interface {
	// Put folds an observation stream into the store. It never blocks the
	// crawler: the caller decides whether to attach a Backend at all.
	Put(ctx context.Context, postings iter.Seq2[*jobposting.JobPosting, error]) (Written, error)

	Query(ctx context.Context, q query.Query, p Page) (Result, error)
	Aggregate(ctx context.Context, a Aggregation) (Table, error)
	Stats(ctx context.Context) (Stats, error)

	io.Closer
}

type Page struct {
	Cursor Cursor // "" is the first page
	Limit  int    // 0 means the backend's default, never "unbounded"
}

// Cursor is opaque and carries the corpus digest it was minted against, so a
// cursor held across a corpus rewrite fails loudly with ErrCursorStale rather
// than silently paging through different data.
type Cursor string

type Result struct {
	Postings []*jobposting.JobPosting
	Next     Cursor
	Total    int  // -1 when the backend cannot say without a second pass
	Complete bool // false when the backend truncated against a budget
}

type Written struct {
	Seen, Inserted, Refreshed int
	Digest                    string // of the resulting corpus
}
```

Aggregation is by explicit dimension and measure, never by caller-supplied SQL —
`docs/architecture-roadmap.md` already requires this of the MCP surface, and
having one rule for both surfaces is what keeps them from diverging.

```go
type Dimension uint8

const (
	ByPlatform Dimension = iota
	ByCompany
	BySource // platform + key
	ByLocation
	ByDepartment
	ByEmploymentType
	ByWorkplaceType
	ByMonthPosted
	ByWeekClosed
)

type Measure uint8

const (
	Count Measure = iota
	CountDistinctCompany
	MedianOpenDays
	P90OpenDays
)

type Aggregation struct {
	Filter  query.Query
	GroupBy []Dimension
	Measure Measure
	Limit   int
}

// Table rows are sorted by Keys, then by Value descending, then by Keys again
// as a tiebreak, so the same Aggregation over the same corpus renders the same
// bytes. Map iteration order must not reach an artifact — the same rule
// `shard plan` and the generated tables already follow.
type Table struct {
	GroupBy []Dimension
	Rows    []Row
}

type Row struct {
	Keys  []string
	Value float64
	N     int
}

type Stats struct {
	Postings, Companies, Sources int
	Oldest, Newest               time.Time
	CorpusDigest                 string
}
```

Implementations, in the order they earn their place:

| package | for | notes |
| --- | --- | --- |
| `storage/memory` | tests, one-shot CLI queries | the default; no file, no dependency |
| `storage/corpus` | the corpus | `.jhtc`; two read modes, below |
| `storage/ndjson` | the snapshots that already exist | read-only, streaming, never loads 780k rows to answer one query |
| `storage/remote` | thin clients against a service | later; the service stays optional |

`storage/corpus` has two read modes behind the same `Backend`:

- **Streaming** — decode the columns a query needs, evaluate, discard. Bounded
  memory. This is what a one-shot CLI invocation uses. Measured: 56 ms for an
  aggregate touching two columns.
- **Resident** — decode the needed columns once into a struct of arrays with
  dictionary IDs for the low-cardinality columns, then answer from memory. This
  is what the TUI, the daemon and the browser tab use. Measured: 325 ms to load
  seven columns, 159.1 MiB of heap, then 3 ms per filtered page and 3 ms per
  aggregate.

The mode is a constructor option, not a separate type, so no caller has to know
which one it got.

## 5. The `.jhtc` format

```
file          := magic column* footer footer_offset magic
magic         := "JHTC" u8:major u8:minor          // currently 00 01
column        := flate(payload)                    // raw DEFLATE, fixed level
footer        := uvarint:ncols entry*
entry         := uvarint:namelen utf8:name u8:encoding
                 uvarint:offset uvarint:complen uvarint:rawlen
footer_offset := u64le                             // offset of `footer`
```

Three payload encodings, chosen per column by the writer and recorded in the
footer:

```
1 dict  := uvarint:ndict (uvarint:len utf8)*  uvarint:nrows uvarint:id*
2 raw   := uvarint:nrows (uvarint:len utf8)*
3 delta := uvarint:nrows zigzag_varint:delta*        // int64
```

Rules that make it a format rather than a serialization:

- **One total row order for every column**: `(platform, source_key, url)`. Row
  *i* of every column is the same posting. `platform` first because it is the
  most common filter and the cheapest dictionary; `url` last because it is the
  identity `internal.Dedupe` already uses, so the order is total.
- **Columns are addressed by name, not position.** A reader skips a column it
  does not know and reads a column the file lacks as the zero value. That is the
  entire schema-evolution story, and it is why adding `Compensation.Provenance`
  later is not a migration. The minor version byte is informational; the major
  byte is the only thing a reader refuses on.
- **No case-folded shadow columns.** Measured: storing `title_fold` cost 18.1%
  of the Parquet corpus. Folding 780k titles at load costs 99 ms once, after
  which substring matching is 13 ms per query instead of 95 ms. The resident
  reader folds at load; the streaming reader folds inline and pays 95 ms on a
  scan that already costs more than that in I/O. Storing the fold is the wrong
  trade in both modes.
- **Determinism is defined over content, not bytes.** `manifest.json` carries
  `content_digest` = SHA-256 over each `(name, encoding, uncompressed payload)`
  in footer order. `shard merge` compares *that*, so a Go release changing
  `compress/flate`'s output cannot break a fail-closed merge. File bytes were in
  fact identical across runs; the digest is what the invariant rests on.

On-disk layout:

```
corpus/
  corpus.jhtc      # postings
  sources.json     # ~3,685 rows of scheduling state, sorted, human-diffable
  runs.ndjson      # one line per run, appended
  manifest.json    # schema version, content_digest, row count, producing commit
```

`sources.json` and `runs.ndjson` are deliberately *not* columnar. They are under
a megabyte (`docs/crawl-budget-model.md` says as much), they are read whole every
time, and being reviewable in a diff is worth more than being fast.

### How a run updates the corpus

**It rewrites it.** Nothing is updated in place. Measured on the Parquet
prototype, which is the pessimistic case because it writes more bytes: read
780k rows, fold in 780k observations keyed by URL, sort, write a new file —
**5.96 s, deterministic, 22.9 MiB output**. Against a 720-second crawl that is
0.8%.

This is the single decision that removes the need for a database. An immutable,
rewritten-every-run corpus needs no transactions, no WAL, no file locking, no
mmap, and no concurrent-writer story — which is exactly the list of things that
broke `js/wasm` for every embedded engine in §1.

It also lands the invariants for free:

- `first_seen` is set once, on insert. `last_seen` advances only for postings
  whose source reached `complete` in this run's manifest. A **failed source
  therefore cannot retire anything**, because retirement is derived
  (`source.last_success > posting.last_seen`) rather than written.
- The merge is still a global union over posting identity, never a sum of
  shards — the rewrite consumes `shard merge`'s existing deduplicated stream.
- A partial run produces a corpus whose manifest says `partial`. Nothing about
  the corpus model weakens the partial-vs-complete distinction; it makes partial
  the normal case, which is what `docs/crawl-budget-model.md` asks for.

## 6. What runs in the browser

Explicitly, because the question was asked explicitly.

**The corpus does, with no VFS and no database.** The file is 19.0 MiB. A PWA
either fetches it once into Cache Storage and keeps it, or reads it with HTTP
range requests: the footer is a 2-read fetch of a few kilobytes, and each column
is one contiguous range. Answering "count by platform over 90 days" is
**2 requests and 2.11 MiB** — measured. GitHub Pages and release-asset CDNs both
serve `Accept-Ranges: bytes`, so this needs no server the project has to own,
which `docs/surfaces-and-extensibility.md` requires.

Resident load under `js/wasm` is **1.06 s and ~159 MiB of heap** for seven
columns, after which queries are 15 ms. That is an interactive experience.

**SQLite-in-WASM with an IndexedDB VFS is a real pattern, and it is not free.**
It is worth being concrete about the bill, since I built enough of it to measure:

- `ncruces/go-sqlite3` is the only pure-Go SQLite that builds for `js/wasm` at
  all (`modernc.org/sqlite` does not, §1), and it works — verified running, not
  merely compiling.
- It costs **+12.15 MiB of binary**. The current toolkit is 11.47 MiB native and
  15.27 MiB / 3.68 MiB-gzipped for `js/wasm`. This would roughly double the
  browser payload.
- The database file is **398.9 MiB** against the corpus's 19.0 MiB — 21x. Over
  IndexedDB that is a quota conversation on every mobile browser.
- There is no filesystem in a browser, so it needs a custom `vfs.VFS`. The
  library ships `readervfs` (read-only over a `ReaderAt`, which an HTTP-range
  fetcher satisfies) and `memdb`; an IndexedDB or OPFS block VFS would have to
  be written. `vfs.SupportsFileLocking` is `false` on `js`, so every connection
  needs `nolock=1` and `db.SetMaxOpenConns(1)`.
- And after all that: 823 ms for the covering-index page, 5.00 s for the
  aggregate, 67.07 s for the median.

The columnar path beats it by 25x on the aggregate and 600x on the median, with
none of the above.

One honest exception: for a **single** cold filtered page and nothing else,
SQLite's 823 ms beats a 1.06 s resident load plus 15 ms. A surface that asks
exactly one question and exits would be marginally better served by SQLite —
and that surface is the CLI, which is not the browser. From the second query
onward the resident path is 55x ahead, and a browser tab is by definition the
many-queries case. I did not separately measure a `.jhtc` *streaming* page read
in wasm, which is the comparison that would settle the one-shot case; Parquet's
equivalent was 451 ms, so it is likely close.

**What the browser cannot do: mutate the corpus.** It does not need to.
`docs/crawl-budget-model.md` gives the browser a live crawl of the CORS-open
majority plus the published corpus as fallback; live results merge into the
resident arrays in memory and are never written back. One writer, still.

## 7. What I rejected, and why

- **`modernc.org/sqlite`.** The roadmap's implied choice. Measured: does not
  build for `js/wasm` or `wasip1` — `modernc.org/libc` has no `js` package.
  26-module graph. This is a hard constraint failure, not a preference.
- **`mattn/go-sqlite3`, DuckDB.** CGO. Excluded by invariant; not benchmarked.
- **`go.etcd.io/bbolt`.** Measured build failure on both wasm targets (mmap).
  Separately, a B+tree KV store gives no aggregate story at all: every query
  shape becomes a hand-rolled index, and "count by platform" becomes a full
  cursor walk with none of a column store's advantages.
- **`cockroachdb/pebble`.** Measured build failure on both wasm targets. 135
  modules. It is an LSM tuned for high-volume random writes; this workload is
  write-once, read-many, rewritten wholesale. Wrong shape twice over.
- **`ncruces/go-sqlite3` — the runner-up among databases, still rejected.** It
  clears the portability gate, which nothing else with SQL does, and it earns
  genuine respect for it. But: 21x the file size, 24x the wasm scan, 600x the
  wasm median, +12.15 MiB of binary, and its best result requires a covering
  index per query shape. Its one unique offering is ad-hoc SQL for humans — and
  `duckdb -c "select platform, count(*) from 'corpus.parquet' group by 1"`
  offers that too, from the exporter, with nothing linked into the binary.
- **`parquet-go` as the corpus — the actual runner-up.** Clears every
  constraint; genuinely good; loses on the numbers in §3 *and* costs 10 linked
  modules (`brotli`, `klauspost/compress`, `pierrec/lz4`, `google/uuid`,
  `bitpack`, `jsonlite`, `twpayne/go-geom`, `google.golang.org/protobuf`,
  `x/sys`, itself). A job-board crawler linking a geometry library and protobuf
  to write a flat table of postings is exactly what constraint 3 says to design
  out. It survives as the **exporter**, in a separate `main` package, which is
  the project's own stated pattern for optional surfaces.
- **NDJSON + a sidecar index, as the corpus.** 375.9 MiB and a 4.80 s full scan;
  10x slower to load and 20x larger than `.jhtc`. A sidecar index would fix the
  point queries and nothing about the aggregates, because the aggregate cost is
  parsing JSON. **But NDJSON is not going away** — it stays the crawl's
  streaming stdout and the per-shard artifact, which is what makes constraint 5
  ("the CLI must keep working with no storage at all") true by construction
  rather than by discipline.
- **ClickHouse.** The roadmap already defers it and the measurements support
  that: an aggregate over the whole corpus is 56 ms. There is no scale argument
  for a server here, and adding one would end the zero-infrastructure property
  that makes every other surface cheap.

## 8. Migration from today's NDJSON snapshots

Nothing in step 0 changes, and the sequence is designed so each step is
independently revertible — the same discipline the sharded cutover documents.

0. **NDJSON stays.** It is the crawl's stdout and the shard artifact.
   `total`, `shard run` and `shard merge` behave identically throughout.
1. **`internal/corpus`.** Writer, reader, the §5 spec as a doc comment, a golden
   file, a round-trip property test over a generator, and a fuzz target on the
   reader. No command uses it yet. This is where the bespoke-format risk is paid
   down, and it should be paid down before anything depends on it.
2. **`total --corpus out.jhtc`, `shard merge --corpus-in prev --corpus-out next`.**
   The merge already computes the global union; this adds `first_seen`/
   `last_seen` and consults each source's terminal state so a failed source
   retires nothing.
3. **Run it beside the existing path for a week.** The corpus row count must
   equal `shard merge`'s deduplicated total, exactly. Two routes to the same
   number that disagree mean one is wrong.
4. **Publish `corpus.jhtc` as a release asset**, not a committed blob —
   `docs/crawl-budget-model.md` item 5.
5. **`storage.Backend` and `postings --corpus`.** This is when the interface in
   §4 lands, with `memory`, `ndjson` and `corpus`.
6. **`tools/corpus-export`** — a separate `main` package, the only thing in the
   repo that imports `parquet-go`, emitting `corpus.parquet`. It doubles as a
   differential test: DuckDB's `select platform, count(*)` over the export must
   match `Aggregate(ByPlatform, Count)` over the corpus.
7. **TUI, MCP, PWA**, on top of 5.

**`corpus rebuild --from snapshots/*.ndjson`** should exist from step 2. The
corpus is derived state; being able to reconstruct it from the immutable
snapshots is what makes step 1's format risk survivable. Choosing wrong costs a
rewrite of one package and a rebuild, not a data migration.

## 9. What I did not measure, and what would change the answer

- **No real browser.** Node's `js/wasm` has a real filesystem, a different GC
  profile, and none of a phone's memory pressure. 159 MiB of heap in a tab is a
  real risk on low-end Android. `docs/surfaces-and-extensibility.md` already
  says the first task of client-side work is re-running its CORS table from an
  actual browser; this belongs in the same session.
- **No GitHub runner.** Same caveat the crawl measurement carries.
- **Synthetic data.** 16 platforms, 22 distinct locations, ~3,500 companies. Real
  location cardinality is far higher, which will grow the `location` dictionary
  and shrink the compression advantage. **Expect the real corpus to exceed
  19 MiB**, possibly substantially. Re-measure on the first real merge; if it
  lands above ~60 MiB the browser download story needs revisiting, though the
  range-request path does not.
- **Timestamps are 44% of `.jhtc`** (8.44 MiB of 19.0 across four columns),
  because the row order is `(platform, source_key, url)` and delta coding gains
  nothing on unsorted values. Sorting by `posted_at` instead would shrink them
  and cost platform pruning. Measured only indirectly: re-sorting the Parquet
  corpus by `(platform, source_key, url)` shrank it 8%. Worth a follow-up, not
  worth blocking on.
- **Corpus growth over history.** One run's worth was measured. A year of
  retained closed postings is an unknown multiple and the retention policy is
  undecided. If the corpus outgrows memory, the resident mode stops being
  viable before the streaming mode does, and row groups become necessary — a
  footer change, which §5 was designed to accommodate.
- **The prototype has no bounds checking.** Production `.jhtc` needs a hostile
  reader: a corrupt `rawlen` currently allocates whatever it says.
- **`.jhtc` has no selective-read story for point queries.** It reads whole
  columns. At 2–3 MiB per column that is fine; if a surface appears that needs
  one posting out of 780k over a network, add row groups.
