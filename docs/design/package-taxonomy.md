# The package taxonomy and the public API

Today `go.mod` declares `github.com/job-hunter-toolkit/job-hunter-toolkit` and
every package lives under `internal/`. Nothing outside the repository can import
a single type. `docs/surfaces-and-extensibility.md` §2 proposes promoting a small
vocabulary — posting, source, query, snapshot, storage — and keeping the crawler
internal. This document turns that sketch into a layout, an import graph, a set
of signatures, and a migration.

Everything measured here was measured on 2026-07-28 in this container, Go
1.26.5, `CGO_ENABLED=0`, plain `go build` with no `-ldflags`. Prototypes are in
the scratchpad, not the repo. Where I did not measure, it says so.

"Nineteen adapters" throughout is the count of `init()` registrations in
`internal/services` at the time of measurement. The number is growing while this
was written; nothing in the design depends on it.

## Recommendation

**Root-level public packages in the existing single module. No `pkg/`, no second
module, and no `init()` registration.**

```
jobposting/   query/   postingio/   snapshot/   source/   sources/<platform>/   storage/
```

Three claims carry the design, and all three are measured:

1. **One module is enough.** A consumer that imports only
   `.../jobposting` gets **no cobra in its `go.mod`, no `go.sum` at all**, and
   builds successfully with an empty module cache and `GOPROXY=off`. Module
   graph pruning already solves the problem a separate SDK module would solve.
2. **The `init()` registry is the expensive part, not the directory names.**
   A program that references exactly one adapter function from
   `internal/services` links **10,696,878 bytes** (native) — within 5 KB of a
   program that uses the entire 3,685-source registry. Split into per-platform
   packages, the same one-source program is **8,943,072 bytes**: −1.75 MB
   native, **−2.45 MB on `js/wasm`**.
3. **Package taxonomy is not the WASM lever; `net/http` is.** A `js/wasm`
   program whose only work is one `http.Get` is already **10,344,455 bytes**.
   The two-platform browser build this project actually wants —
   Greenhouse + Ashby, 48% of the registry and both CORS-open — is 12,062,180
   bytes, of which 86% is `net/http`. Splitting the sources is worth doing and
   buys 10–18%; it does not make a 10 MB WASM binary small, and no layout
   choice can while adapters take a `*http.Client`.

## 1. The measurements

### 1.1 Binary ladder

Each row is a complete `main` package built for two targets. The point of the
ladder is that the interesting deltas are between adjacent rows, not the
absolute numbers.

| probe | what it links | linux/amd64 | js/wasm |
| --- | --- | ---: | ---: |
| `hello` | empty `main` | 1,829,975 | 1,845,153 |
| `jp` | posting types + `Filter`, **stdlib only** | 2,461,396 | 2,703,223 |
| `httpimp` | + `_ "net/http"`, never called | 5,138,978 | 4,820,452 |
| `core` | today's `internal` (posting + filter + `All`) | 5,514,382 | 5,315,950 |
| `nethttp` | one live `http.Get` | 8,503,843 | 10,344,455 |
| `customrt` | `http.Client` with a custom `RoundTripper` | 8,504,257 | 10,346,077 |
| `htmlp` | + `golang.org/x/net/html` | 8,811,522 | 10,782,212 |
| `split` | **1 adapter**, per-platform packages | 8,943,072 | 10,971,192 |
| `split2` | **2 adapters** (greenhouse + ashby), per-platform | 9,691,948 | 12,062,180 |
| `one` | **1 adapter** via today's `internal/services` | 10,696,878 | 13,425,626 |
| `svc` | the **whole registry** via `internal/services` | 10,691,961 | 13,419,863 |
| — | the CLI as it ships today | 12,098,241 | 15,346,740 |

`gzip -9` of the `js/wasm` builds, which is what a browser actually downloads:
`split2` 3,157,347 · `svc` 3,452,632 · CLI 3,876,609.

Five conclusions:

- **`one` ≈ `svc`, within 4,917 bytes.** Importing a single adapter from
  `internal/services` links all nineteen, because `init()` in any file of a
  package runs for every importer of that package. This is the tree-shaking
  claim, measured.
- **Per-platform packages recover 2.45 MB of `js/wasm` for one adapter and
  1.36 MB for two** (13,425,626 → 10,971,192; 13,419,863 → 12,062,180). The
  saving shrinks as you link more platforms, which is the honest shape of it.
- **The floor is `net/http`.** `customrt` shows that supplying your own
  `RoundTripper` and never touching `http.DefaultTransport` saves nothing —
  10,346,077 vs 10,344,455 bytes. On `js/wasm` the two-platform browser build is
  12.06 MB of which 10.34 MB is "a program that can do an HTTP GET".
- **Merely importing `net/http` costs 3.31 MB native / 2.98 MB `js/wasm`**
  (`httpimp` − `hello`), without calling it. This is the whole argument for
  keeping `jobposting` free of `net/http`: a corpus reader, a manifest parser or
  a filter has no business linking an HTTP stack.
- **The adapter cost is code, not data.** The company slug tables across
  `internal/services` are 6,662 entries and 192,156 bytes of source text. The
  ~2.9 MB `js/wasm` delta from `nethttp` to `svc` is nineteen decoders, not
  slugs. Generating the registry into an embedded blob would save nothing worth
  having.

### 1.2 Does a consumer pull cobra?

Two throwaway modules: `example.com/lib` with a leaf package importing nothing
and a `cmd` package importing cobra; `example.com/app` importing only the leaf.

```
$ go mod tidy && cat go.mod
module example.com/app
go 1.24.0
require example.com/lib v0.0.0
replace example.com/lib => ../lib

$ cat go.sum
(absent — the file was never created)

$ GOTOOLCHAIN=local GOMODCACHE=$EMPTY GOPROXY=off go build ./...
(success)
```

The consumer's `go.mod` names no cobra, no `go.sum` exists, and the build
completes with an empty module cache and the proxy off, proving cobra is never
fetched. `go list -m all` still *reports* cobra, because it walks the graph, but
nothing is downloaded, verified, compiled or linked.

That settles the "separate SDK module?" question on the dependency grounds it is
usually argued on. §7 covers the one remaining argument for splitting.

## 2. `pkg/`, root-level, or a second module

**Root-level packages, one module.**

`pkg/` is rejected. Russ Cox opened
[golang-standards/project-layout#117](https://github.com/golang-standards/project-layout/issues/117)
on 2021-04-09 to say the repository is not a standard and that "the _vast_
majority of packages in the Go ecosystem do _not_ put the importable packages in
a `pkg` subdirectory." The technical effect of `pkg/` here would be exactly one
extra path element on every import and zero change to compilation, linking, or
visibility. There is no argument for it that survives contact with the
measurements above.

The layout question that *does* have content is: the root package is
`package main` today, and `README.md` line 20 documents

```console
$ go install github.com/job-hunter-toolkit/job-hunter-toolkit@latest
```

which only works when the module root is a `main` package. So the root stays
`main`, and the public packages sit beside it. That is not a compromise; it is
common. Hugo is `package main` at its module root with `hugolib/`, `config/`,
`markup/`, `resources/`, `tpl/` and a dozen more importable root-level packages,
and installs with `go install github.com/gohugoio/hugo@latest`.

One cleanup that follows: the root currently holds `main.go` (1,197 lines),
`shard_cmd.go`, `merge_cmd.go`, `enrich_cmd.go`, `crawl_report.go` and their
tests. Those move to `internal/cli`, leaving a ~15-line `main.go` at the root.
This is a pure move, it preserves `go install …@latest`, and it stops the root
directory from being a mixture of a command and a library index.

## 3. The tree

```
job-hunter-toolkit/
├── main.go                     package main — 15 lines, calls internal/cli
│
├── jobposting/                 PUBLIC. The record. stdlib only, no net/http.
│   ├── posting.go              JobPosting, PostingSource, EmploymentType,
│   │                           WorkplaceType, normalizers, IsRemote/IsHybrid
│   ├── compensation.go         Compensation, Period, Provenance, AnnualMin/Max,
│   │                           MoreTrustedThan
│   └── seq.go                  Seq (iter.Seq2[*JobPosting, error]), Dedupe
│
├── query/                      PUBLIC. → jobposting
│   └── query.go                Query, Match, Apply, IsZero
│
├── postingio/                  PUBLIC. → jobposting
│   └── writer.go               Writer interface + JSONL/JSON/CSV/text writers
│
├── snapshot/                   PUBLIC. → jobposting
│   ├── postings.go             Record, Writer, Reader, DedupeIdentity, PostingKey
│   └── manifest.go             Manifest, SourceRun, SchemaVersion, Read/Write
│
├── source/                     PUBLIC. → jobposting, snapshot, net/http
│   ├── source.go               Source, Set, JobsFunc, Keyed, KeyedNamed, Merge
│   ├── all.go                  All, AllWithConcurrency, DefaultConcurrency
│   ├── client.go               NewHTTPClient + ClientOption (wraps internal/httpx)
│   └── observe.go              Observe → snapshot.SourceRun
│
├── sources/                    PUBLIC. One package per ATS.
│   ├── greenhouse/greenhouse.go    Platform, Companies, Jobs, Sources
│   ├── ashby/ashby.go
│   ├── workday/workday.go
│   ├── … 16 more …
│   ├── direct/                     was internal/companies (oxide, uber)
│   └── all/all.go                  Sources() — explicit, no init()
│
├── storage/                    PUBLIC. → jobposting, query. See storage-engine.md
│   ├── storage.go              Backend, Page, Cursor, Result, Aggregation, Stats
│   ├── memory/                 the default
│   ├── ndjson/                 read-only over existing snapshots
│   └── corpus/                 .jhtc
│
├── internal/
│   ├── cli/                    the cobra commands (was the repo root)
│   ├── httpx/                  the limiter and retry policy. Never public.
│   ├── ats/                    fetchJSON/fetchHTML/pagination for built-in adapters
│   ├── paydetect/              pay parsing from prose and markup (needs x/net/html)
│   ├── shard/                  plan / affinity / cost / merge
│   ├── enrich/                 company enrichment + its generator
│   └── tests/                  test helpers
│
└── tools/enrichgen/
```

Nineteen `sources/<platform>` packages is a lot of directories. It is also
exactly the granularity at which a consumer chooses what to link, which is the
only reason to have it. `sources/all` exists so that the CLI, which wants
everything, says so in one line.

## 4. The import graph

Depth is four layers plus `main`. Every edge points down; there are no upward
edges, so the graph is trivially acyclic.

```
                        ┌───────────────┐
  L0  stdlib only       │  jobposting   │   no net/http, no x/net, no deps
                        └───────┬───────┘
              ┌────────────┬────┴───────┬──────────────┐
              ▼            ▼            ▼              ▼
  L1      ┌───────┐   ┌─────────┐  ┌──────────┐  ┌──────────┐
          │ query │   │postingio│  │ snapshot │  │ storage  │──▶ query
          └───┬───┘   └─────────┘  └────┬─────┘  └────┬─────┘
              │                         │             │
              │                         ▼             ▼
  L2          │                    ┌────────┐   storage/{memory,ndjson,corpus}
              │                    │ source │──▶ net/http, internal/httpx
              │                    └───┬────┘
              │                        ▼
  L3          │              sources/<platform> ──▶ internal/ats, internal/paydetect
              │                        ▼
  L4          │                  sources/all
              │                        │
              └────────────────────────┴──────────▶ internal/cli ──▶ main
```

Rules, and how each is enforced:

- **`jobposting` imports nothing outside the standard library, and not
  `net/http`.** Enforced by a test that runs `go list -deps ./jobposting` and
  fails on any path containing a dot or equal to `net/http`. Verified in the
  prototype: the package's transitive deps are `cmp errors io iter math/bits
  slices strings sync sync/atomic syscall time unicode unicode/utf8 unsafe
  runtime`.
- **`query`, `postingio` and `snapshot` do not import `source`.** A manifest
  reader, a corpus query and a CSV writer must not link an HTTP stack. Measured
  cost of getting this wrong: 2.98 MB on `js/wasm`.
- **No public package names an internal type in an exported signature.**
  `source.NewHTTPClient` returns `*http.Client`, never `*httpx.something`; that
  is what keeps the limiter free to change without a compatibility event. Guarded
  by a test that runs `go doc -all` over each public package and fails on a line
  matching a known internal qualifier (`httpx.`, `ats.`, `shard.`, `paydetect.`,
  `enrich.`). This is a grep, not a proof — `go doc` renders the package
  qualifier, which is unique per internal package, and that is enough to catch
  the mistake in review.
- **The layer order above is asserted mechanically.** A test decodes
  `go list -json -deps ./...` and fails when a package at layer *n* imports one
  at layer ≥ *n*. `encoding/json` and `os/exec`, no new module.

Public packages *may* import `internal/` — they are in the same module, and
`source` importing `internal/httpx` is the entire point of the boundary. The
rule is about signatures, not imports.

## 5. The public API

Only the signatures. Doc comments are omitted here; in the tree every one of
these carries the pre-v1 disclaimer from §7.

### `jobposting`

```go
package jobposting

type JobPosting struct {
	Company  string `json:"company"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Location string `json:"location"`

	Compensation   *Compensation `json:"compensation,omitempty"`
	Remote         *bool         `json:"remote,omitempty"`
	Department     string        `json:"department,omitempty"`
	Team           string        `json:"team,omitempty"`
	EmploymentType EmploymentType `json:"employment_type,omitempty"`
	WorkplaceType  WorkplaceType  `json:"workplace_type,omitempty"`
	Seniority      string        `json:"seniority,omitempty"`
	PostedAt       time.Time     `json:"posted_at,omitzero"`
	UpdatedAt      time.Time     `json:"updated_at,omitzero"`
	RequisitionID  string        `json:"requisition_id,omitempty"`
	ExternalID     string        `json:"external_id,omitempty"`
	Source         PostingSource `json:"source,omitzero"`
}

func (j *JobPosting) IsRemote() bool
func (j *JobPosting) IsHybrid() bool

type PostingSource struct {
	Platform string `json:"platform,omitempty"`
	Key      string `json:"key,omitempty"`
}

func (s PostingSource) IsZero() bool

type EmploymentType string
type WorkplaceType string
type Period string
type Provenance string

func EmploymentTypeValues() []EmploymentType
func WorkplaceTypeValues() []WorkplaceType
func NormalizeEmploymentType(raw string) (EmploymentType, bool)
func NormalizeWorkplaceType(raw string) (WorkplaceType, bool)

type Compensation struct {
	Min, Max   float64
	Currency   string
	Period     Period
	Summary    string
	Provenance Provenance
}

func (c *Compensation) IsZero() bool
func (c *Compensation) AnnualMin() (float64, bool)
func (c *Compensation) AnnualMax() (float64, bool)
func (c *Compensation) MoreTrustedThan(other *Compensation) bool

// Seq is a stream of postings. The first error is not necessarily the end.
type Seq = iter.Seq2[*JobPosting, error]

// Dedupe removes postings that share an identity. Errors pass through.
func Dedupe(seq Seq) Seq
```

The struct tags are the load-bearing part. They are already the de-facto wire
format in NDJSON output and the README advertises piping it into `jq`; §7 freezes
them ahead of everything else in this document.

### `query`

```go
package query

type Query struct {
	Titles, ExcludeTitles, Locations, Companies, Departments []string
	Remote, HasCompensation                                  bool
	MinAnnual                                                float64
	EmploymentTypes []jobposting.EmploymentType
	WorkplaceTypes  []jobposting.WorkplaceType
	PostedSince     time.Time
}

func (q Query) Match(p *jobposting.JobPosting) bool
func (q Query) Apply(seq jobposting.Seq) jobposting.Seq
func (q Query) IsZero() bool
```

`query.Query` stutters. It is still the right name: the package is the query
language, the type is a query, and the standard library does this whenever the
package has exactly one central type — `list.List`, `ring.Ring`,
`regexp.Regexp`, `template.Template`. `docs/design/storage-engine.md` already
writes `Query(ctx, q query.Query, p Page)`, and one name across two documents is
worth more than avoiding a repeated word.

### `source`

```go
package source

type Source struct {
	Platform string   // ATS family: "greenhouse", "workday", …
	Key      string   // tenant identifier the adapter fetches with
	Company  string   // human-facing name derived from Key
	Jobs     JobsFunc
}

type JobsFunc func(context.Context, *http.Client) jobposting.Seq

type Set []Source

func Merge(sets ...Set) Set
func (s Set) Matching(terms ...string) Set   // pure filter, no reordering
func (s Set) Platforms(names ...string) Set
func (s Set) Companies() []string            // deduplicated, sorted
func (s Set) Sorted() Set                    // by Platform, then Key
func (s Set) Interleaved() Set               // round-robin by platform

// Keyed turns a per-tenant fetch function into one Source per key. This is the
// helper a third-party adapter uses; it is `multiJobsFunc` promoted.
func Keyed(platform string, jobs func(context.Context, *http.Client, string) jobposting.Seq, keys ...string) Set
func KeyedNamed(platform string, jobs func(context.Context, *http.Client, string) jobposting.Seq, name func(string) string, keys ...string) Set

// All fetches every source, up to DefaultConcurrency at a time. Order is
// deliberately unspecified — see the determinism note below.
var DefaultConcurrency = 64

func All(ctx context.Context, client *http.Client, set Set) jobposting.Seq
func AllWithConcurrency(ctx context.Context, client *http.Client, limit int, set Set) jobposting.Seq

// Observe wraps a set with lifecycle measurement and returns a snapshot of the
// results so far. Safe to call while a crawl runs.
func Observe(set Set, logger *slog.Logger) (Set, func() []snapshot.SourceRun)

// NewHTTPClient returns a client carrying this project's retry and per-service
// politeness policy. The policy itself is internal and changes without notice;
// only the *http.Client is public.
func NewHTTPClient(opts ...ClientOption) *http.Client

type ClientOption func(*clientConfig) // clientConfig is unexported on purpose

func WithUserAgent(ua string) ClientOption
func WithLogger(l *slog.Logger) ClientOption
func WithProxies(urls ...*url.URL) ClientOption
```

`ClientOption` is a function over an **unexported** struct. That is the seam:
`internal/httpx` keeps `WithMaxAttempts`, `WithBaseDelay`, `WithMaxDelay`,
`WithPerHostLimit` and `WithTransport`, and none of them become a compatibility
surface. Publishing the limiter would freeze a politeness policy that
`docs/architecture-roadmap.md` requires to stay adjustable, and
`docs/surfaces-and-extensibility.md` §4 already forbids a second source of truth
for rate limits.

Today `services.SourcesMatching` filters *and* interleaves in one call. Splitting
that into `Matching` and `Interleaved` is behaviour-preserving at the call site
(`set.Matching(terms...).Interleaved()`) and stops a filter from silently
reordering a crawl.

**Determinism note.** `source.All` is the one public function in this document
whose output order is deliberately unspecified: it is a concurrent fan-in and
postings arrive as they are fetched. Every artifact writer downstream —
`snapshot`, `shard merge`, the generated tables — must sort or key-dedupe after
it, which is what they already do. This is stated in the doc comment so nobody
discovers it from a flaky golden file.

### `snapshot`

Extracted from `internal/shard`, unchanged in behaviour and unchanged on disk.

```go
package snapshot

const SchemaVersion = 2
const PostingKeyBytes = 16

type Record struct {
	Key     string                 `json:"key"`
	Posting *jobposting.JobPosting `json:"posting"`
}

func DedupeIdentity(p *jobposting.JobPosting) string
func PostingKey(p *jobposting.JobPosting) string

type Writer struct{ /* … */ }
func NewWriter(w io.Writer) *Writer
func (w *Writer) Write(p *jobposting.JobPosting) error
func (w *Writer) Written() int
func (w *Writer) Flush() error

type SourceRun struct {
	Platform, Key, Company, Status string
	StartedAt, FinishedAt          time.Time
	DurationMS                     int64
	Postings, Errors               int
	ErrorClass                     string
}

type Manifest struct { /* exactly today's shard.Manifest fields */ }

func (m Manifest) Complete() bool
func (m Manifest) UnfinishedSources() []SourceRun
func ReadManifest(path string) (Manifest, error)
func DecodeManifest(r io.Reader, name string) (Manifest, error)
func WriteManifest(path string, m Manifest) error
```

`SourceRun` lives here rather than in `source` on purpose: it is a manifest
record, and putting it in `source` would drag `net/http` into every program that
reads a manifest — 2.98 MB on `js/wasm`, measured. `source.Observe` therefore
returns `[]snapshot.SourceRun`, giving the edge `source → snapshot` and none
back.

The JSON field names are byte-identical to today's `services.SourceRun` and
`shard.Manifest`, so **no schema version bump and no artifact change**. Only the
Go type names move.

### `postingio`

Extracted from `main.go`'s `postingOutput`, `postingColumn` and
`newPostingPrinter`.

```go
package postingio

// A Writer emits postings in some encoding. Implementations must be
// deterministic: the same postings in the same order produce the same bytes.
type Writer interface {
	Write(*jobposting.JobPosting) error
	Flush() error
}

func NewJSONL(w io.Writer) Writer
func NewJSONArray(w io.Writer) Writer
func NewText(w io.Writer) Writer
func NewCSV(w io.Writer, columns []Column, header bool) (Writer, error)

type Column struct {
	Name  string
	Value func(*jobposting.JobPosting) string
}

func CoreColumns() []Column
func ExtendedColumns() []Column
func ParseColumns(spec string) ([]Column, error)
```

`Column.Value` is a plain function, so a third-party column is a closure, and
`ParseColumns` handles the CLI's `--columns` string. Column order is the slice
order; nothing here iterates a map.

### `storage`

Specified in `docs/design/storage-engine.md` §4 and not restated here. The only
taxonomy constraints it must satisfy: `storage` imports `jobposting` and `query`
and nothing else from this module; backends live in `storage/<name>`; and no
`Backend` is constructed unless a surface asks for one, which is what keeps
invariant 5 (the CLI works with no storage) true by construction rather than by
discipline.

## 6. Extension points

All compile-time. `docs/surfaces-and-extensibility.md` §4 rules out dynamically
loaded Go plugins — platform-locked, version-brittle, impossible in WASM — and a
consumer of this project is building a binary anyway.

### 6.1 A third party adds a job source

The complete extension. No registry, no `init()`, no interface to satisfy.

```go
package acmeats // github.com/someone/acme-ats-adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/source"
)

const Platform = "acme"

// Jobs fetches one tenant's postings.
func Jobs(ctx context.Context, client *http.Client, tenant string) jobposting.Seq {
	return func(yield func(*jobposting.JobPosting, error) bool) {
		url := "https://api.acme.example/v1/" + tenant + "/jobs"

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			yield(nil, fmt.Errorf("acme %q: %w", tenant, err))
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			yield(nil, fmt.Errorf("acme %q: %w", tenant, err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			yield(nil, fmt.Errorf("acme %q: unexpected status %s", tenant, resp.Status))
			return
		}

		var doc struct {
			Jobs []struct{ ID, Title, Location, URL string } `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			yield(nil, fmt.Errorf("acme %q: %w", tenant, err))
			return
		}

		for _, job := range doc.Jobs {
			if !yield(&jobposting.JobPosting{
				Company:    tenant,
				URL:        job.URL,
				Title:      job.Title,
				Location:   job.Location,
				ExternalID: job.ID,
				Source:     jobposting.PostingSource{Platform: Platform, Key: tenant},
			}, nil) {
				return
			}
		}
	}
}

// Sources exposes the adapter as one Source per tenant.
func Sources(tenants ...string) source.Set {
	return source.Keyed(Platform, Jobs, tenants...)
}
```

Using it:

```go
set := source.Merge(greenhouse.Sources(), acmeats.Sources("initech", "hooli"))

for posting, err := range source.All(ctx, source.NewHTTPClient(), set) {
	// …
}
```

The adapter depends on `jobposting` and `source` and nothing else. It gets the
project's retry and per-service rate limiting for free because it uses the
`*http.Client` it is handed — the rule `docs/adding-a-source.md` already states
("Do not build your own HTTP client") now applies to out-of-tree adapters
unchanged.

**What a third party does not get:** `internal/ats`'s `fetchJSON` error-wrapping
conventions and `internal/paydetect`'s prose pay parser. Both are heuristics this
project must stay free to change, and neither is needed to write a correct
adapter. See §10 for the open question this leaves.

### 6.2 A third party adds a storage backend

```go
type Backend struct{ /* … */ }

func (b *Backend) Put(ctx context.Context, seq jobposting.Seq) (storage.Written, error)
func (b *Backend) Query(ctx context.Context, q query.Query, p storage.Page) (storage.Result, error)
func (b *Backend) Aggregate(ctx context.Context, a storage.Aggregation) (storage.Table, error)
func (b *Backend) Stats(ctx context.Context) (storage.Stats, error)
func (b *Backend) Close() error
```

Attached at the surface, never by the crawler:

```go
store, err := mybackend.Open("…")
written, err := store.Put(ctx, source.All(ctx, client, set))
```

Nothing in `source` or `sources/*` mentions `storage`, which is the mechanical
guarantee behind "the CLI must keep working with no storage at all".

### 6.3 A third party adds an output format

```go
type parquetWriter struct{ /* … */ }

func (w *parquetWriter) Write(p *jobposting.JobPosting) error { … }
func (w *parquetWriter) Flush() error                          { … }

var _ postingio.Writer = (*parquetWriter)(nil)
```

Wired in their own `main`. `storage-engine.md` deliberately keeps a Parquet
exporter out of the default binary; `postingio.Writer` is the seam that lets it
be a separate `main` package rather than a fork.

### 6.4 Registration: explicit assembly, not `init()`

`sources/all` is the entire built-in registry, written out:

```go
package all

import (
	".../sources/ashby"
	".../sources/bamboohr"
	// … 17 more …
	".../source"
)

// Sources returns every source compiled into this package, sorted by platform
// and then key so the order is a written-down fact rather than a link-order
// accident.
func Sources() source.Set {
	return source.Merge(
		ashby.Sources(),
		bamboohr.Sources(),
		// …
	).Sorted()
}
```

Three things this buys, and one it costs.

- **Tree-shaking.** Measured: −2.45 MB `js/wasm` for a one-adapter build,
  −1.36 MB for the two-platform (Greenhouse + Ashby) browser build that
  `docs/surfaces-and-extensibility.md` §1 identifies as 48% of the registry.
- **The set is explicit.** `go doc sources/all` lists it. Today `Builtin` is
  whatever happened to be linked, and a package that fails to be imported
  silently contributes nothing.
- **The order is chosen, not inherited.** Today registry order is `init()` order,
  which is file order within the package, which is filename order — and filenames
  do not match platform names (`oracle_orc.go` → `oraclecloud`, `ashbyhq.go` →
  `ashby`). `.Sorted()` replaces that with a stated rule.
- **It costs a second edit.** Adding a platform means a new package *and* a line
  in `sources/all`. Mitigation: a test in `sources/all` that reads the
  `sources/` directory and fails when a subdirectory is not represented in
  `Sources()`. The repo already uses this shape of guard —
  `TestFilterFieldsAreWiredIn` walks `Filter` by reflection so a new field cannot
  reach `main` unwired.

**Is sorting safe for `shard plan`?** Yes, and this was checked rather than
assumed. `shard.SourceSetID` sorts identities before hashing;
`shard.Build` sorts refs within each affinity group (`plan.go:204`), sorts bins
(`plan.go:215`) and sorts each shard's sources (`plan.go:243`). Registry order
does not reach a plan ID or a shard assignment. What it does reach is crawl order
within a shard, via `Plan.Resolve`, which preserves registry order — a scheduling
detail, not an artifact.

## 7. Compatibility

**Pre-v1, the Go API carries no promise.** Every public package's doc comment
opens with the same sentence, which is also what
`docs/surfaces-and-extensibility.md` §2 asks for:

```go
// Package jobposting …
//
// This package is pre-v1: its Go API may change in any release without a
// deprecation period. The JSON encoding of JobPosting is stable — see
// docs/design/package-taxonomy.md §7.
```

**Frozen ahead of the Go API,** because artifacts and shell pipelines already
depend on them:

| Frozen | Why |
| --- | --- |
| `jobposting.JobPosting` JSON field names and `omitempty`/`omitzero` choices | already the NDJSON wire format; `README.md` documents piping it to `jq` |
| `snapshot.Manifest` JSON, and `SchemaVersion` | `shard merge` fails closed on a schema mismatch |
| `platform + key` as source identity | `docs/architecture-roadmap.md` names it the stable integration ID |
| the `.jhtc` header and footer | `docs/design/storage-engine.md` §5 |

Changing any of those is a schema-version event with a migration, at any version.

**Free to change pre-v1:** Go type and function names, `query.Query` fields,
adapter function signatures, everything under `internal/`, and the set of
`sources/<platform>` packages.

**How it is documented:** `go run golang.org/x/exp/cmd/apidiff@<pinned>` against
the previous tag, as a CI step. This matches the pattern `ci.yml` already uses
for `staticcheck` (line 286) and `govulncheck` (line 306) — `go run tool@version`
compiles the tool without adding a module requirement. Pre-v1 the step is
informational and prints the diff into the job summary; at v1 it becomes a
failing gate on incompatible changes. Deprecations use `// Deprecated:` and
survive one minor release.

**Should the SDK be a separate module?** No.

The usual reason to split is "so a consumer does not pull cobra", and §1.2 shows
the consumer does not: no `go.mod` entry, no `go.sum`, and a successful build
with an empty module cache and the proxy off. Go 1.17 module graph pruning
already delivers what the split would deliver.

The one argument that survives is versioning: a second module could reach
`sdk/v1` while the CLI stays v0. That is real, and it is not worth two `go.mod`
files, `replace` directives in every development checkout, two tagging schemes
and a permanent risk of the two drifting. **Revisit only when the public API is
ready for v1 and the CLI is not** — at that point the split is a one-commit
change, because the import paths inside the module do not move when a
subdirectory becomes a module.

Two smaller notes. `github.com/picatz/iters` is imported by exactly one file,
`internal/services/helpers_test.go`, so it is test-only and pruning keeps it out
of a consumer's graph for the same reason cobra stays out. And `go.mod`'s
`go 1.25.0` directive is a real constraint on consumers regardless of module
layout: a caller on an older toolchain cannot import any of this. Splitting would
not change that.

## 8. Ergonomics

If these are not short, the API is wrong.

**Crawl one company.**

```go
client := source.NewHTTPClient()

for posting, err := range greenhouse.Jobs(ctx, client, "anthropic") {
	if err != nil {
		log.Print(err)
		continue
	}
	fmt.Println(posting.Title, "—", posting.Location)
}
```

**Crawl a set, filtered, deduplicated, as NDJSON.**

```go
set := source.Merge(greenhouse.Sources(), ashby.Sources()).Interleaved()
q := query.Query{Titles: []string{"security"}, Remote: true}

w := postingio.NewJSONL(os.Stdout)
defer w.Flush()

for posting, err := range q.Apply(jobposting.Dedupe(source.All(ctx, source.NewHTTPClient(), set))) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err) // structured data to stdout, diagnostics to stderr
		continue
	}
	if err := w.Write(posting); err != nil {
		log.Fatal(err)
	}
}
```

**Query a stored corpus.**

```go
store, err := corpus.Open("corpus.jhtc")
if err != nil {
	log.Fatal(err)
}
defer store.Close()

res, err := store.Query(ctx, query.Query{Titles: []string{"go"}}, storage.Page{Limit: 50})
for _, posting := range res.Postings {
	fmt.Println(posting.Company, posting.Title)
}
```

**Crawl everything, the way the CLI does.**

```go
set := all.Sources().Matching(companyTerms...).Interleaved()
observed, results := source.Observe(set, logger)

seq := source.AllWithConcurrency(ctx, client, concurrency, observed)
// … write postings …

manifest := snapshot.Manifest{Sources: results(), /* … */}
```

## 9. Migration

Nine steps. Each compiles, each passes the existing tests, each is separately
reviewable, and none changes an artifact byte until step 6 — which changes only
crawl order within a shard, not any output.

The technique throughout is the **type-alias shim**. `internal` keeps existing:

```go
package internal

type JobPosting = jobposting.JobPosting
type Filter     = query.Query
type Jobs       = jobposting.Seq
type JobsFunc   = source.JobsFunc

const EmploymentTypeFullTime = jobposting.EmploymentTypeFullTime
// …

func All(ctx context.Context, c *http.Client, fns ...JobsFunc) Jobs { … }
```

Type aliases carry method sets, so `internal.Filter{…}.Match(p)` keeps compiling
untouched. Constants must be re-declared; functions get one-line wrappers rather
than `var All = source.All`, so `go doc` on the shim points at the new home.

| # | Step | Mechanical? |
| --- | --- | --- |
| 1 | `main.go`, `shard_cmd.go`, `merge_cmd.go`, `enrich_cmd.go`, `crawl_report.go` + tests → `internal/cli/`; root `main.go` becomes a 15-line shim | yes — `git mv`, package rename |
| 2 | `internal/job_posting.go` → `jobposting/posting.go`; the type half of `internal/compensation_text.go` → `jobposting/compensation.go`; the `Jobs` alias and `Dedupe` → `jobposting/seq.go`; parsers → `internal/paydetect/` | **no** — see below |
| 3 | `internal/filter.go` → `query/query.go` (`Filter` → `Query`), `IsRemote`/`IsHybrid`/`matchesAnyWorkplaceType` stay on the posting in `jobposting` | yes |
| 4 | `internal/shard/postings.go` + `manifest.go` → `snapshot/`; `services.SourceRun` → `snapshot.SourceRun` | yes |
| 5 | `internal/services/builtin.go` + `observe.go` + `all.go` → `source/`; `json.go` + `pagination.go` → `internal/ats/` | mostly — `Set` methods and `NewHTTPClient` are new |
| 6 | 19 × `internal/services/<platform>.go` → `sources/<platform>/`; `internal/companies` → `sources/direct`; delete `registerBuiltin` and 19 `init()`s; add `sources/all` | **no** — the registration change |
| 7 | `main`'s `postingOutput`/`postingColumn`/`newPostingPrinter` → `postingio/` | mostly |
| 8 | `storage/` and its backends | new code |
| 9 | delete every shim; `internal` and `internal/services` disappear | yes |

### Where step 2 is not mechanical

`internal/compensation_markup.go` imports `golang.org/x/net/html`, and
`jobposting` must not (§4). The obvious split — move `compensation_markup.go` to
`internal/paydetect` and leave `compensation_text.go` behind — **does not
compile**, which I found by trying it. The two files share unexported helpers:
`normalizeText`, `moneyRangePattern`, `currencyForMatch`, `parseMoney`, `group`,
`lowAmountGroup`, `lowMagnitudeGroup`, `minPlausibleAnnual`, `maxPlausibleAnnual`.

The split that does compile runs along **types versus parsers**, not text versus
markup:

- `jobposting/compensation.go` keeps `Compensation`, `Period`, `Provenance`,
  `trustRank`, `periodsPerYear`, `effectivePeriod`, `IsZero`, `AnnualMin`,
  `AnnualMax`, `MoreTrustedThan` — `internal/compensation_text.go` lines 1–68,
  minus the `minPlausibleAnnual`/`maxPlausibleAnnual` constants, which are parser
  state.
- `internal/paydetect` takes the rest of `compensation_text.go` (from
  `const maxRangeRatio` at line 70) plus all of `compensation_markup.go`, and
  exports `ParseCompensationFromText` and `ParseCompensationFromDescription`
  returning `*jobposting.Compensation`.

I built this arrangement in the scratchpad; `jobposting` and `internal/paydetect`
both compile, and `jobposting`'s transitive dependency set is stdlib-only with no
`net/http` and no `golang.org/x/net`.

### Where step 6 is not mechanical

Three real changes, not renames:

1. **`registerBuiltin` and the 19 `init()` functions are deleted.** Each
   `<platform>.go` gains `func Sources(keys ...string) source.Set` returning
   `source.Keyed(Platform, Jobs, keys...)`, defaulting to the package's
   `Companies` slice when no keys are given.
2. **`services.Greenhouse` becomes `greenhouse.Jobs`**, and so on for the other
   eighteen. The shim keeps `services.Greenhouse` alive until step 9.
3. **`services.Builtin` (a `var`) becomes `all.Sources()` (a `func`).** The shim
   can keep `var Builtin = all.Sources()`, but note that anything importing the
   shim links every adapter — which is fine, because after step 1 the only
   importer is `internal/cli`, and the shim is deleted in step 9.

Order changes from init-order to `(platform, key)`. Verified safe for artifacts
in §6.4: plan IDs and shard assignments are computed from sorted input.

I converted `greenhouse.go` and `ashbyhq.go` to this shape in the scratchpad. Both
built first-or-second try, and the resulting packages compile for `linux/amd64`,
`linux/arm64`, `darwin/arm64`, `windows/amd64`, `js/wasm` and `wasip1/wasm` with
`CGO_ENABLED=0`, and pass `go vet`. The conversion is a per-file
search-and-replace (`internal.` → `jobposting.`, `fetchJSON` → `ats.Fetch`,
`jsonRequest` → `ats.Request`, drop `init()`), so eighteen more of them is tedious
rather than risky.

### Portability and dependency budget

- **No new modules.** The direct dependency set stays cobra and
  `golang.org/x/net`, with `shoenig/test` and `picatz/iters` test-only. `apidiff`
  is `go run`, not a requirement.
- **No CGO anywhere.** Nothing proposed here touches cgo, mmap, syscalls or file
  locking; `snapshot` and `storage/ndjson` are `io.Reader`/`io.Writer` over paths
  the caller supplies, and `storage/corpus`'s portability is
  `storage-engine.md`'s problem.
- **WASM stays green.** Confirmed across all six targets on the prototype
  packages. The full CLI already builds for `js/wasm` and `wasip1/wasm` today
  (15,346,740 / 15,256,346 bytes), and none of these moves changes a build
  constraint.

## 10. What I rejected, and why

- **`pkg/`.** No technical effect, one extra path element per import, and Russ
  Cox's [#117](https://github.com/golang-standards/project-layout/issues/117)
  says the ecosystem does not do it. Rejected on both counts.
- **A separate SDK module.** §1.2: a consumer importing a leaf package already
  gets no cobra in `go.mod`, no `go.sum`, and builds offline. The split would buy
  independent versioning at the cost of two release processes; revisit at v1.
- **Moving the CLI to `cmd/job-hunter-toolkit`.** Breaks the documented
  `go install github.com/job-hunter-toolkit/job-hunter-toolkit@latest`. Hugo
  shows root-`main`-plus-root-packages is normal.
- **Keeping `init()` registration.** Measured 2.45 MB of `js/wasm` for a
  one-adapter build and 1.36 MB for the two-platform browser build, plus an
  implicit set and a link-order-derived registry order.
- **A global mutable registry with `source.Register(…)`,** in the
  `database/sql` / `image.RegisterFormat` style. Same tree-shaking cost as
  `init()`, plus a mutable global whose contents depend on which packages a
  binary happens to import, plus a determinism hazard the project has already
  been bitten by elsewhere (`shard plan` sorts precisely because iteration order
  once leaked into an artifact).
- **`type Source interface { Jobs(ctx, client) Seq }`.** A one-method interface
  where a func type will do. The struct-with-a-func-field already exists, is
  satisfied by a closure, and carries `Platform`/`Key`/`Company` which an
  interface would have to add as three more methods.
- **Publishing `httpx`.** Freezes the per-service politeness policy that
  `docs/architecture-roadmap.md` requires to stay adjustable, and
  `docs/surfaces-and-extensibility.md` §4 forbids a second source of truth for
  rate limits. `source.NewHTTPClient` hands out `*http.Client` instead.
- **Publishing `internal/ats` (`fetchJSON`, `fetchHTML`).** These encode
  decisions about retry classification, error wrapping and HTML tolerance that
  change whenever a board misbehaves. A third-party adapter can use
  `encoding/json` directly, which `docs/adding-a-source.md` already assumes.
- **Publishing the adapters' response structs.** Nineteen boards' JSON shapes
  would become a compatibility surface, and half of them are polymorphic across
  tenants (`docs/adding-a-source.md` on Jibe's `meta_data`).
- **Publishing `internal/shard`.** Encodes an affinity and bin-packing policy
  derived from `httpx`'s own limiter table. It reads `snapshot` and `source`;
  nothing reads it.
- **Generating the registry into an embedded blob to shrink the binary.** The
  6,662 slugs are 192,156 bytes of source text. The size is nineteen decoders,
  not data.
- **A `jobposting.Writer` living in `jobposting`.** It would put
  `encoding/json`, `encoding/csv` and `fmt` into the vocabulary package that
  every consumer links. `postingio` is one more directory and keeps `jobposting`
  at zero encoding dependencies.
- **One flat root package.** The root is `package main`, and a single package
  cannot express the layering that makes `jobposting` linkable without
  `net/http`.

## 11. What I did not measure, and open questions

- **`sources/all` was not built.** I converted two adapters, not nineteen, so
  "the CLI's binary size is unchanged" is reasoning (the same code is linked),
  not a measurement. Someone should check it after step 6.
- **The tree-shaking saving for a realistic browser set is bounded above by
  1.36 MB.** That is the measured Greenhouse+Ashby number. A browser build that
  wanted six platforms would save less. The saving is real but small next to the
  10.34 MB `net/http` floor.
- **The `net/http` floor is the actual WASM problem, and this document does not
  solve it.** `JobsFunc` takes a `*http.Client`, and a custom `RoundTripper` does
  not help (10,346,077 vs 10,344,455 bytes). Making a browser build meaningfully
  smaller would require a different adapter contract — a byte-level fetch
  function rather than an `*http.Client` — which changes every adapter signature
  and belongs to whoever designs the WASM surface, not here. Flagging it so that
  work does not start by assuming the taxonomy already fixed it.
- **Out-of-tree adapters cannot use `internal/paydetect`.** Built-in adapters
  parse pay from description prose; a third-party adapter cannot. That asymmetry
  is deliberate for now — "nothing goes public without a consumer" — but it is
  the first thing a serious external adapter will ask for. Promoting `paydetect`
  is a later, cheap decision.
- **`go doc`-grep is a guard, not a proof.** The "no internal type in a public
  signature" rule is checked by matching internal package qualifiers in `go doc`
  output. A type aliased into a public package would slip past it. A real check
  needs `go/types` with export data or `golang.org/x/tools/go/packages`, and I am
  not proposing a new module for it.
- **No live behaviour was re-verified.** Every measurement here is a build or a
  module resolution. The migration's claim of "no behaviour change" rests on the
  existing test suite, which I did not run against a converted tree beyond the
  two adapters.
