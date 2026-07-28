# The query layer and the `index` command

`main.go` already has a query language. Eleven flags — title, exclude-title,
location, company, remote, has-pay, min-pay, department, employment-type,
workplace-type, posted-since — OR within a flag and AND across flags, executed
by `internal.Filter.Match` against a live crawl. That is the de-facto language
and this document does not replace it. It formalises it as a value type, gives
it a total order and a cursor so it can be paged, and answers the question the
flags have never had to face: what does that language cost against 780,489
stored postings instead of a stream?

The answer decides whether this project needs an index at all.

It is a query layer and one optional derived file, not a database. Nothing here
is required by the crawler; `postings`, `total` and `health` keep working with
no corpus, no index and no storage, as `docs/architecture-roadmap.md` requires.

## What this design rests on

Everything below was measured on a 4-vCPU Intel Xeon @ 2.10 GHz container, Go
1.25/1.26 linux/amd64, and — for the browser numbers — Node 22.22 (V8), the
same engine Chrome ships. The prototype is `scratchpad/queryproto`; its full
output is `scratchpad/queryproto/RESULTS.txt`.

**Real posting text.** The same `scratchpad/all.ndjson` the corpus format
design used: 31,473 postings pulled from live boards across 19 platforms.
Measured on it directly: 44 companies, 4,916 distinct locations, 7,963 distinct
titles, 131 departments; **28.5 bytes of title and 24.5 bytes of location per
posting**. Those two numbers are the ones that decide the size of everything
below, and they are real.

**A corpus the size of the real one.** 780,489 rows, inflated from that sample
by replicating whole sources under new keys, written in the layout
`docs/design/corpus-format.md` §3.2/§3.3 specifies: source-major order,
block-gzipped NDJSON, 256 KiB blocks. Result: 390.7 MB raw, 48.3 MB gzipped,
1,489 blocks. That is within a few percent of the 414 MB / 51.6 MB the corpus
design measured across 153 files, so the scan costs below are the right
magnitude for the real thing.

**Three caveats that constrain what may be claimed from it.**

- **Cardinality is understated.** The sample has 44 companies and 44 sources;
  the real registry has 3,462 companies and 3,685 sources. Dictionary *sizes*
  below are therefore artificially small. Dictionary sizes turn out not to
  matter — they are 0.0 MB of a 92.9 MB artifact — and dictionary *resolution*
  is one pass over the dictionary per query, so 3,685 entries instead of 1,078
  costs microseconds. But the numbers are not a census.
- **Title text repeats more than reality.** Replication means the same title
  appears many times. This does not flatter the scan numbers, because the arena
  stores every row's bytes separately and `bytes.Contains` cost is dominated by
  bytes scanned, not by content. It *would* flatter a dictionary-encoded title
  column, which is exactly why this design does not propose one.
- **Match counts are properties of the synthesised corpus,** not predictions
  about the real one. "49,923 postings match `--title engineer`" means the
  benchmark had that selectivity, nothing more.

**One file, not 153.** The prototype writes the corpus as a single file. That
understates how much file pruning buys, so every "no index needed here" claim
below is conservative.

---

## 1. The query language

### 1.1 It stays exactly what it is

The eleven predicates keep their current semantics, including case-insensitive
substring matching on title, location, company and department. Three reasons,
in order of weight:

1. **Changing them silently changes every existing invocation.** A user's
   `--title go` is in a shell alias, a cron line, a README. Substring matching
   is a documented contract in `internal/filter.go`'s doc comments, and
   `TestFilterFieldsAreWiredIn` already exists to stop a field drifting out of
   sync. This is not the place to break it.
2. **Honouring it exactly is cheap.** Measured: 14–20 ms to evaluate a
   substring predicate against all 780,489 rows (§4). A tokenised alternative
   would buy 15 ms and cost the language its phrase queries, its short terms
   and its negation (§7).
3. **The alternatives are worse for this data.** Job titles are short noun
   phrases in dozens of house styles. `--title "staff engineer"` is a phrase;
   `--title go` is two characters; `--exclude-title manager` is a negation. An
   inverted index serves none of the three without a verification scan.

The one real defect in substring matching is that `--title go` matches
"Cargo" and "Chicago". §7.4 proposes the fix, and it is not an index.

### 1.2 `query.Query`

```go
// Package query is the vocabulary every surface shares: a CLI flag set, an MCP
// tool call, a URL query string and a TUI filter panel all build the same
// value, and every backend executes it the same way.
//
// Until v1 this package carries no compatibility promise.
package query

// Query is a conjunction of independent predicates. Within a slice field the
// terms are OR-ed; across fields they are AND-ed. The zero value matches
// everything.
//
// It is deliberately not a tree. There is no nesting, no OR across fields, no
// arithmetic and no user-supplied regular expression, because every one of
// those turns "can this backend run this query?" from a yes into a question,
// and there are five backends.
type Query struct {
	Titles        []string // substring, case-insensitive
	ExcludeTitles []string // substring, case-insensitive, applied after Titles
	Locations     []string
	Companies     []string
	Departments   []string // matched against Department OR Team

	// Sources selects exact integrations. Unlike Companies this is equality on
	// platform+key, which is what lets a planner prune whole corpus files
	// before reading a byte. `--company` cannot do that: it is a substring
	// match against a derived short name.
	Sources []jobposting.PostingSource

	Remote          bool
	HasCompensation bool
	MinAnnual       float64
	EmploymentTypes []jobposting.EmploymentType
	WorkplaceTypes  []jobposting.WorkplaceType
	PostedSince     time.Time

	// States restricts to corpus lifecycle states. Empty means the backend's
	// default, which is Open and Stale — a search for work should not return
	// closed postings unless asked. A live backend ignores it: everything a
	// crawl just fetched is open by construction.
	States []State
}

// State is the corpus lifecycle vocabulary, defined here rather than in
// `corpus` so that `query` depends on nothing but `jobposting`. The corpus
// package computes these; this package only names them.
type State string

const (
	StateOpen   State = "open"
	StateStale  State = "stale"
	StateClosed State = "closed"
	StateLapsed State = "lapsed"
)

// Match is the predicate half of the language: pure, allocation-light, and
// usable on a live crawl stream with no storage anywhere. It is
// internal.Filter.Match, moved and made public.
func (q Query) Match(p *jobposting.JobPosting) bool

// Apply filters a posting iterator, replacing internal.Filter.Apply. This is
// the whole of what the storage-free CLI needs.
func (q Query) Apply(jobs jobposting.Jobs) jobposting.Jobs

// IsZero reports whether the query constrains nothing.
func (q Query) IsZero() bool
```

`Sources` is the only genuinely new predicate, and it exists because paging and
file pruning need an exact integration selector that `--company` cannot be.
Today `postingFilterFor` deletes `Companies` before matching, precisely because
comparing a tenant key against a derived company name silently returned zero
postings. Over a corpus, `Companies` becomes a real predicate again and
`Sources` is the exact one; the live path keeps its current behaviour, where
`--company` narrows which sources are crawled.

### 1.3 Three front ends, one table

The CLI flag name, the URL query key and the MCP argument name must never
drift. The project already has a rule for this: `internal/shard` derives its
affinity groups from `httpx`'s own policy table rather than a curated list
beside it, because two lists drift and the drift looks like a bug somewhere
else. The same rule applies here.

```go
// Fields returns the query language as data, in a fixed order. It is the
// single source a cobra flag set, a URL encoder, an MCP input schema and the
// help text are all built from. Adding a predicate means adding one entry.
func Fields() []Field

type Field struct {
	Flag string // "exclude-title" — the CLI flag and the URL key
	JSON string // "exclude_title" — the MCP argument name
	Help string
	Kind Kind // KindTerms, KindBool, KindMoney, KindTime, KindEnum, KindSource
	Enum []string // the closed vocabulary, for KindEnum
}

// Value is one field's accessor on a particular Query. It is shaped like
// flag.Value and pflag.Value on purpose, so main.go can adapt it in three
// lines and this package can stay free of a flag dependency — the
// "a public type may not transitively expose an internal one" rule from
// docs/surfaces-and-extensibility.md, applied to third-party packages too.
type Value interface {
	Set(raw string) error // one occurrence; repeats OR together
	String() string       // canonical rendering, for help and for Values()
	Type() string         // "terms" | "bool" | "money" | "time" | "enum"
	IsBool() bool         // so a CLI knows --remote takes no argument
}

func (q *Query) Field(flag string) (Value, bool)
```

Encoding and parsing:

```go
// Values renders the canonical URL form. Keys are sorted; a field with no
// constraint is absent. Round-trips: Parse(q.Values(), any) equals
// q.Normalize().
func (q Query) Values() url.Values

// Parse builds a Query from URL values or MCP arguments. `now` resolves
// relative ages such as "30d"; passing it explicitly is what keeps a Query a
// value with no clock inside it, the same discipline
// docs/design/corpus-format.md imposes with one RunAt per run.
func Parse(v url.Values, now time.Time) (Query, error)

// Normalize returns the canonical form: every term trimmed, lowercased,
// deduplicated and sorted; enum slices deduplicated and put in
// EmploymentTypeValues order; MinAnnual <= 0 zeroed; PostedSince truncated to
// a second in UTC. Idempotent.
func (q Query) Normalize() Query
```

A canonical form is not tidiness. `--title Go --title go --title " go "` is one
constraint, and unless it encodes as one constraint the cursor fingerprint in
§3 changes when the user's shell history does.

```
?title=go&title=golang&exclude-title=sales&location=remote
 &min-pay=180000&posted-since=2026-06-28T00:00:00Z&employment-type=full_time
 &order=first_seen&limit=50&cursor=AQhK...
```

The same document as MCP arguments:

```json
{"title": ["go", "golang"], "exclude_title": ["sales"], "location": ["remote"],
 "min_pay": 180000, "posted_since": "2026-06-28T00:00:00Z",
 "employment_type": ["full_time"], "order": "first_seen", "limit": 50}
```

### 1.4 Totality

"Small and total" is a property worth stating as invariants a test can check:

- **Every syntactically valid `Query` is executable by every backend.** There is
  no predicate a backend may decline. This is what makes five backends
  tractable and it is why regular expressions are excluded — a SQL backend
  cannot run Go's RE2 and a browser should not run an attacker's backtracking
  pattern.
- **Cost is O(rows considered).** No joins, no cross products, no correlated
  subqueries, no `ORDER BY` over an expression.
- **Adding a field can only narrow.** `IsZero()` matches everything and each
  additional constraint removes rows. `TestFilterFieldsAreWiredIn` already
  walks the struct by reflection and fails when a field is missing from `Match`
  or `IsZero`; it extends to `Fields()`, `Values()` and `Normalize()`, so a new
  predicate cannot reach `main` half-wired.
- **Parsing is the only place errors occur.** After `Parse` returns, execution
  cannot fail for a reason inherent to the query.

Explicitly not in the language: free boolean expressions, regex, relevance
scoring, `OR` across fields, sorting by an expression, projection, aggregation
beyond the bounded facets in §5.3, and anything resembling SQL. The roadmap's
own words for the MCP surface — "analytical tools should accept explicit
dimensions and limits rather than arbitrary SQL by default" — are the rule
here too.

---

## 2. Ordering

Paging needs a total order. Four orders, each ending in the corpus row id so
that ties are broken deterministically and every order is total:

| `Order` | sorts by | why it exists |
| --- | --- | --- |
| `OrderFirstSeen` **(default)** | `first_seen` desc, `id` asc | The only date the corpus has for **every** row. |
| `OrderPostedAt` | `posted_at` desc, unknown last, `id` asc | What the board says, when it says anything. |
| `OrderPay` | annualised max desc, unknown last, `id` asc | The one numeric ranking a job hunter asks for. |
| `OrderID` | `id` asc | The stable full-walk order for machine consumers. |

**`OrderFirstSeen` is the default, not `OrderPostedAt`,** and that is a
deliberate consequence of the data. Most boards publish no publication date at
all — it is why `Filter.PostedSince` documents excluding undated postings — so
`posted_at` desc puts a large, unknown-sized tail beyond any page a human will
reach. `first_seen` is written by the corpus for every row on the run that
first observed it, so ordering by it is total, and "appeared in the corpus
recently" is very nearly the question a job hunter is asking.

The sort key is one `int64` in every order, encoded so that ascending integer
comparison is the intended order:

- `first_seen` / `posted_at` descending: `-unixSeconds` / `-unixNanos`, with
  unknown mapped to `math.MaxInt64` so it sorts last. Values are clamped to
  guard the pre-1678 wraparound.
- pay descending: the order-preserving `float64`→`int64` transform (flip the
  sign bit for non-negatives, invert all bits for negatives), then negated.
  **Not** a truncation to whole currency units: truncating would let two rows
  a cent apart tie and be ordered by id instead of by pay, which is a silent
  wrong answer in the one order that exists to rank by a number.
- `OrderID`: key is always 0; the 16-byte id does all the work.

**`OrderTitle` is rejected.** Alphabetical-by-title is not a question anyone
asks of a job board, and it is the one order whose key is not a scalar, which
would force the cursor to carry a variable-length string.

Measured: all four orders cost the same, because none of them is served by a
sorted structure. Ordering is a bounded top-K selection over the scan
(§4.3), and the comparator is a couple of integer compares.

```
first_seen desc          14.1ms
posted_at desc           14.3ms
pay desc                 14.1ms
id (stable full walk)    13.7ms
```

---

## 3. Pagination

Mandatory from the first commit, per `docs/surfaces-and-extensibility.md`.

### 3.1 What a cursor encodes

```go
// Page is the reading half of a request: everything about how results come
// back, and nothing about which rows match. Keeping it separate from Query is
// what makes Query.Fingerprint meaningful.
type Page struct {
	Order  Order
	Limit  int    // backend clamps to [1, MaxLimit]; MaxLimit is 1000
	Cursor Cursor // zero value means the first page
	Count  bool   // compute Result.Total; see §3.4
	Facets []Dim  // see §5.3
}

// Cursor is an opaque position in one ordered result set.
//
// It encodes no offset, no row number, no byte position and no block index.
// That is the property that makes it survive a compaction: `corpus compact`
// rewrites every file and moves every byte, but it preserves row ids and it
// does not change first_seen, posted_at or compensation. A cursor made of a
// sort key and an id therefore still points at exactly the same place in the
// order.
type Cursor struct {
	// unexported; construct with ParseCursor or take one from a Result
}

func (c Cursor) String() string // base64url, unpadded, 46 characters
func (c Cursor) IsZero() bool
func ParseCursor(s string) (Cursor, error)
```

Wire format, 34 bytes:

```
byte  0      cursor format version (1)
bytes 1..8   fingerprint of (Query.Normalize().Values(), Page.Order)
byte  9      corpus IdentityVersion the cursor was minted under
bytes 10..17 int64 sort key of the last row emitted, big-endian
bytes 18..33 that row's 16-byte corpus id
```

The cursor is opaque to clients and it is checked, not trusted:

```go
var (
	// ErrCursorMismatch means the cursor was minted for a different query or a
	// different order.
	ErrCursorMismatch = errors.New("query: cursor does not match this query")

	// ErrCursorIdentity means the corpus was rebuilt under a new
	// IdentityVersion, which renumbered every row. Re-run the query.
	ErrCursorIdentity = errors.New("query: cursor predates a corpus rebuild")
)
```

Both fail closed. This is the behaviour AIP-158 specifies — "the user is
expected to keep all other arguments to the RPC the same; if any arguments are
different, the API **should** send an `INVALID_ARGUMENT` error" — and the
fingerprint is what lets this implementation actually detect it rather than
document it. Paging with a mutated filter is otherwise a silent wrong answer,
and this project's stated preference is that a plausible wrong answer is worse
than an error.

### 3.2 What survives, and what does not

| event | cursor | why |
| --- | --- | --- |
| `corpus compact` | **survives** | Ids and sort-key fields are unchanged; only bytes move. |
| a new run: rows appear, change, close | **survives** | §3.3. |
| the index is rebuilt, dropped, or goes stale | **survives** | The index is a cache; it changes speed, never order. |
| a corpus rebuild bumping `IdentityVersion` | **rejected** | Every id changed. `ErrCursorIdentity`. |
| a different query or order | **rejected** | `ErrCursorMismatch`. |

The `IdentityVersion` byte is what makes the third row honest. The corpus
design writes a `renames.jsonl.gz` mapping old ids to new on a rebuild, so a
cursor *could* be translated. This design does not: a rebuild is a rare,
announced event, re-issuing a query costs one scan, and a translation table on
the read path is a second place for identity to be wrong.

### 3.3 The guarantee, and the honest limits of it

> Within one paging session, **no row is returned twice, and no row that
> matched the query for the whole session is skipped.**

Rows that appear mid-session are returned if they sort after the current
position and missed if they sort before it. Rows that close mid-session simply
stop appearing. Neither is a defect that can be fixed without a snapshot
isolation this format does not have and should not buy; both are strictly
better than offset paging, which under the same conditions duplicates rows and
skips them.

Verified in the prototype rather than asserted: page 1 of a 1,393-match query,
then 199 matching rows deleted, then paging forward with the page-1 cursor over
the mutated corpus.

```
1393 matches before mutation, 199 rows dropped mid-session, 24 pages walked
duplicates across pages: 0
surviving matching rows never returned: 0
```

`Result.Generation` carries the corpus generation the page was answered from,
so a client that walked pages across a run boundary can *see* that it did. It
is reported, not enforced; refusing a cursor because the corpus advanced would
make paging useless on a daemon that refreshes continuously.

### 3.4 Totals

`Page.Count` is opt-in, and it is exact when set, because with the scan image
the count is a byproduct of the scan the page already performs — the same
loop, one increment. `Result.Total` is `-1` when `Count` is false or when the
backend stopped at a budget. AIP-158 permits exactly this shape (`total_size`
as an optional field).

A TUI sets `Count` and shows "1–50 of 49,923". An MCP tool answering under a
token budget leaves it off and pays nothing.

---

## 4. What a query costs, measured

This section exists to be read before §5. The point of it is that most queries
do not need an index, and it is worth knowing which.

### 4.1 With no index at all

Streaming the corpus, gunzipping, `json.Unmarshal` per row, `Query.Match`:

| query | matches | native | js/wasm |
| --- | ---: | ---: | ---: |
| `--title "security engineer"` | 1,393 | 5.09 s | — |
| `--title engineer` | 49,923 | 4.70 s | **19.9 s** |
| `--remote --title go,golang,rust --min-pay 180000` | 525 | 5.53 s | — |
| `--company vercel` | 1,975 | 5.08 s | — |
| no filter | 780,489 | 4.79 s | — |

**152,000 rows/s native, 39,000 rows/s in js/wasm.** JSON decoding is
essentially all of it: `Query.Match` on already-decoded rows is what the image
numbers in §4.2 measure at 2–20 ms. The corpus format design measured the same
thing from the other direction — 130–142k rows/s to decode the whole corpus —
so this is a stable property of the format, not of this prototype.

Five seconds is not a query. Twenty seconds in a browser is not a product.

### 4.2 With no index, but with the corpus layout doing its job

The corpus is 153 files, one bucket per platform, and a source never spans two
files. A query naming a company or a source therefore reads one file.

Measured: **one 8,000-row corpus file (523 KB gzipped) scans in 50.5 ms.**

That is the whole answer for a large class of queries. At 152k rows/s:

| query shape | rows read | unindexed cost |
| --- | ---: | ---: |
| `--source greenhouse:anthropic` | ~8,000 (one file) | **51 ms** (measured) |
| `--company` naming one employer | ~8,000 (one file) | **51 ms** |
| one small platform (`ashby`, 12,772 postings) | ~13,000 | ~85 ms |
| one large platform (`jibe`, 202,926 postings) | ~203,000 | ~1.3 s |
| **anything not naming a company, source or platform** | 780,489 | **5 s** |

**So: the index is earned by exactly one query shape** — a text, pay, date or
category filter with no company, source or platform to prune by. That shape
happens to be the flagship job-hunting query ("remote senior Go roles, anywhere,
paying over $180k"), which is why the index is worth building. It is not worth
building for anything else.

### 4.3 With the scan image

Same battery, against the derived image of §5, single-threaded:

| query | matches | corpus scan | image scan | speedup |
| --- | ---: | ---: | ---: | ---: |
| `--title "security engineer"` | 1,393 | 5.09 s | 19.9 ms | 256x |
| `--title engineer` | 49,923 | 4.70 s | 16.9 ms | 279x |
| `--remote --title go,… --min-pay` | 525 | 5.53 s | **2.0 ms** | 2769x |
| `--company vercel` | 1,975 | 5.08 s | 2.6 ms | 1965x |
| `--location "new york"` | 17,113 | 5.11 s | 15.8 ms | 323x |
| no filter | 780,489 | 4.79 s | 2.9 ms | 1662x |
| `--posted-since 30d` | 369,828 | 4.86 s | 3.2 ms | 1524x |
| five predicates at once | 200 | 4.83 s | 2.3 ms | 2087x |
| `--department engineering` | 4,372 | 4.98 s | 3.4 ms | 1473x |
| `--workplace-type remote` | 23,597 | 5.19 s | 16.4 ms | 317x |

Two shapes are visible and they are the design in miniature. Queries with no
text predicate cost **2–3.4 ms**: that is 780,489 iterations of integer
comparisons, the floor of the loop. Queries with a text predicate cost
**14–20 ms**: the extra 12–17 ms is `bytes.Contains` over a 41.4 MB arena,
about 2.5 GB/s, which is memory bandwidth and not much else.

Same image under js/wasm in V8, which is the browser story:

| query | js/wasm, one page of 50 |
| --- | ---: |
| `--title "security engineer"` | 72.9 ms |
| `--title engineer` | 98.1 ms |
| `--remote --title go,… --min-pay` | 14.2 ms |
| `--company vercel` | 18.0 ms |
| no filter | 32.3 ms |
| five predicates at once | 17.1 ms |

4–5x slower than native and still interactive. Against 19.9 s unindexed in the
same runtime, that is a 200x difference between a browser tab that works and
one that does not.

### 4.4 Paging is flat

A page is one scan with a bounded heap, so page 200 costs what page 1 costs.
This is the property offset paging cannot have.

```
page 1:   20.6ms
page 200: 17.7ms

limit=50    14.3ms
limit=500   15.6ms
limit=5000  19.2ms
```

### 4.5 Hydration

The image answers *which* rows match and *in what order*. It does not carry the
URL or the original-case text, so a page is hydrated from the corpus. Because
each image shard is positionally aligned with its corpus file, a matching row's
position **is** its row ordinal in that file, and the corpus's own
`blocks.json.gz` turns an ordinal into a byte range. Only those ranges are read.

| page | distinct blocks | bytes read | time |
| --- | ---: | ---: | ---: |
| `--title "security engineer"`, 50 rows | 41 | 1,687 KB | 37.9 ms |
| `--title engineer`, 50 rows | 44 | 1,660 KB | 37.7 ms |
| `--company vercel`, 50 rows | 4 | 141 KB | **3.5 ms** |
| no filter, 50 rows | 40 | 1,489 KB | 34.4 ms |

Bounded by the page size, not by the corpus size, and cheap when the page
clusters. **Total local page latency is therefore about 20 ms of query plus
35 ms of hydration**, and the hydration half parallelises across four cores if
it ever matters.

This is the design's honest weak point in a browser: 41 scattered blocks is 41
HTTP range requests. They are independent and go out at once over HTTP/2, but
it is 41 requests. The alternative — a display sidecar carrying URL and
original-case title and location, about 113 B/row, roughly doubling the
artifact — is measured enough to reject for now and cheap enough to add later
if a real PWA says so. It is listed in §9 rather than built.

---

## 5. The scan image

### 5.1 What it is, and what it is not

It is a columnar, fixed-width projection of every predicate in the language,
plus the row id. It is **not** an index in the sorted-structure sense: there is
no B-tree, no skip list, no posting list, and no ordering other than the
corpus's own. It does not make the scan avoidable. It makes the scan 250x
cheaper by removing JSON from it.

That distinction is the whole design. The measured problem in §4.1 is not that
780,489 predicate evaluations are expensive — they take 2.9 ms — it is that
decoding 780,489 JSON objects to reach them takes 4.8 s. The fix for a slow
scan is a faster scan.

### 5.2 Layout

One image shard per corpus file, at `index/<corpus path>.jhti`. Columns, in
file order, each length-prefixed:

```go
type Image struct {
	N int

	ID        []byte    // N*16, the corpus row id: hydration and cursor tiebreak
	PostedAt  []int64   // unix nanoseconds; math.MinInt64 == the board said nothing
	FirstSeen []int64   // unix seconds
	Annual    []float64 // Compensation.AnnualMax(); valid iff flagHasAnnual
	TextOff   []uint32  // offset into Arena
	TitleLen  []uint16
	LocLen    []uint16
	Company   []uint32  // dictionary ids
	Dept      []uint32
	Team      []uint32
	SourceID  []uint32
	Flags     []uint16

	// Arena holds strings.ToLower(title) then strings.ToLower(location) for
	// each row, contiguously. Lowercasing the haystack once at build time is
	// the single largest win in the format: today every query calls
	// strings.ToLower on every field of every row, allocating each time.
	Arena []byte

	// Dictionaries are per shard, so a shard is self-contained: it can be
	// rebuilt, fetched or verified with no reference to any other shard.
	CompanyDict, DeptDict, TeamDict, SourceDict []string // lowercased
}

const (
	flagRemote    = 1 << 0 // precomputed JobPosting.IsRemote()
	flagHybrid    = 1 << 1 // precomputed JobPosting.IsHybrid()
	flagHasWP     = 1 << 2 // the board published a structured workplace type
	flagHasComp   = 1 << 3 // !Compensation.IsZero()
	flagHasPosted = 1 << 4
	flagHasAnnual = 1 << 5
	// bits 8..10: employment type code; bits 11..12: workplace type code
)
```

Header, 120 bytes:

```
"JHTIDX"          magic, 6 bytes
uint16            image format version
uint64            rows
[32]byte          the corpus file's FileMeta.SHA256 — of the UNCOMPRESSED bytes
[32]byte          sha256(identity_version ‖ corpus format_version)
uint64            corpus generation, advisory
[32]byte          sha256 of everything after the header
```

### 5.3 Executing against it

```go
// Plan is a Query compiled against one shard's dictionaries. Every
// low-cardinality predicate collapses into a membership test resolved once per
// shard, so the per-row cost of `--company anthropic` is an array index.
type Plan struct{ /* unexported */ }

func (im *Image) Compile(q query.Query) *Plan
func (p *Plan) Match(im *Image, row int) bool

// Page runs one scan and selects the next `limit` matches after `cur` with a
// bounded max-heap of size `limit`. O(n log limit) time, O(limit) memory, and
// the same cost at any depth.
func (im *Image) Page(p *Plan, o query.Order, cur query.Cursor, limit int) (rows []int, next query.Cursor, total int)
```

`Plan.Match` evaluates cheapest-first — flags, then integers, then dictionary
ids, then the arena — because the arena is the only part that touches more than
a machine word per row. That ordering is why the five-predicate query in §4.3
(2.3 ms) is *faster* than the one-predicate broad title query (16.9 ms): the
selective integer predicates run first and most rows never reach the text.

Facets, when a TUI asks for them, are counters incremented in the same loop:

```go
// Dim is a facet dimension. Only low-cardinality, dictionary-encoded or
// enumerated fields are permitted; title and location are not dimensions and
// never will be.
type Dim string

const (
	DimPlatform Dim = "platform"; DimCompany Dim = "company"
	DimDepartment Dim = "department"; DimEmploymentType Dim = "employment_type"
	DimWorkplaceType Dim = "workplace_type"; DimState Dim = "state"
)
```

At most 5 dimensions per request, at most 50 values each, sorted by count
descending then value ascending so the result is deterministic under ties.

### 5.4 It is a lossless projection, and that is testable

Every predicate in §1.2 is decidable from the image alone, exactly:

| predicate | column | exact? |
| --- | --- | --- |
| `Remote` | `flagRemote`, precomputed from `IsRemote()` | yes |
| `HasCompensation` | `flagHasComp` | yes |
| `MinAnnual` | `flagHasAnnual` + `Annual` as `float64` | yes — not truncated to cents |
| `PostedSince` | `flagHasPosted` + `PostedAt` in nanoseconds | yes |
| `EmploymentTypes` | 3-bit code | yes — a closed vocabulary of 6 |
| `WorkplaceTypes` | 2-bit code + `flagHasWP` + remote/hybrid bits | yes — including the fallback rule |
| `Titles`, `ExcludeTitles` | arena, pre-lowercased | yes |
| `Locations` | arena | yes |
| `Companies`, `Departments` | dictionaries of lowercased values | yes |
| `Sources` | `SourceDict` | yes |

Two details that would quietly break it if got wrong, both handled: `Annual` is
a `float64` and not an integer, so a `--min-pay` boundary compares identically
either way; and an empty department string is excluded from the resolved
dictionary set, matching `containsAny("", terms) == false`.

The prototype checks this end to end rather than arguing it. For all ten
queries it builds the reference page by scanning the corpus, sorting matches,
and taking the first 50, and compares against the image's page — total match
count, row ids, and order:

```
verifying image pages against corpus scan (order=first_seen desc, limit=50)
  all 10 queries agree, ids and order included
```

**`TestIndexAgreesWithScan` is the single most important test in this design.**
It is what licenses mixing indexed and unindexed shards in one query (§6.3), and
it is what makes "delete the index at any time" safe rather than hopeful.

### 5.5 Size and build cost

92.9 MB for 780,489 rows: **119.0 B/row**, against 390.7 MB / 500.6 B/row for
the corpus it derives from — 24%.

| column | size | B/row |
| --- | ---: | ---: |
| text arena (lowercased title + location) | 41.4 MB | 53.0 |
| id | 12.5 MB | 16.0 |
| company / dept / team / source ids | 12.5 MB | 16.0 |
| posted_at | 6.2 MB | 8.0 |
| first_seen | 6.2 MB | 8.0 |
| annual max | 6.2 MB | 8.0 |
| text offsets | 3.1 MB | 4.0 |
| title and location lengths | 3.1 MB | 4.0 |
| flags | 1.6 MB | 2.0 |
| dictionaries | 0.03 MB | 0.0 |
| **total** | **92.9 MB** | **119.0** |

The arena is 45% of it and is 53.0 B/row because real titles are 28.5 B and
real locations 24.5 B. That is the irreducible part: the language matches
substrings against those two fields, so those two fields have to be present.

Gzipped at level 1 for publication: **18.5 MB, 23.7 B/row** — 5.0x, and
smaller than the 30.3 MB / 38.8 B/row that `docs/design/corpus-format.md`
measured for the slim NDJSON search index it sketches. See §9.

Build cost:

```
index build end to end: 9.377s (decode 7.945s, project 1.227s, write 205ms)
```

**Nine seconds against a 720-second crawl: 1.3%.** Decoding the corpus is 85%
of it, which means the marginal cost of building the image during
`corpus apply` — when the rows are already decoded — is the 1.2 s projection
step and nothing else. That is the right place to build it, and it makes the
usual case incremental for free: `apply` touches only the files a run refreshed,
so only those shards are rebuilt. A bounded run refreshing a fifth of the
sources rewrites a fifth of the image.

The prototype's 1,436 MB peak heap is an artefact of buffering every row; the
real builder streams a file at a time and its peak is one shard, which for the
largest corpus file (`jibe/fedex`, 108,041 rows) is 12.9 MB of image plus the
rows in flight. Per-shard dictionaries are what make that true: no global state
means no global buffer.

### 5.6 Determinism

Dictionary ids are assigned in row order, rows are already in the corpus's
`(key, id)` order, and there is no map in the serialised form. Verified:

```
two builds of 50,000 rows are byte-identical: c1a056750260c221
```

Unlike the corpus's gzip members, the image has no compressor in it, so
byte-identity here does not depend on the Go toolchain's `compress/flate`
staying stable. The published, gzipped copy inherits that caveat and is checked
by the hash of the uncompressed bytes, exactly as the corpus format already
does.

---

## 6. The `index` command

### 6.1 Contract

```
index build   --corpus DIR [--force]   # build or refresh shards whose input changed
index verify  --corpus DIR             # check every shard's hashes; exit non-zero on damage
index drop    --corpus DIR             # delete index/, which loses nothing
index stat    --corpus DIR             # shards, freshness, bytes, rows
```

Four properties, in order of how much they matter:

1. **It is an optimisation and never a requirement.** Every query answerable
   with an index is answerable without one, at the costs in §4.1 and §4.2. No
   command fails because `index/` is absent, and none prints an error about it.
2. **It is a pure derived cache.** Every byte is reconstructible from the
   corpus. `index drop` followed by any query returns the same rows in the same
   order, more slowly. `rm -rf index/` is a supported operation and the test
   suite does it.
3. **A stale shard cannot be used.** Not "is detected and warned about" —
   *cannot be used*, by construction. See §6.2.
4. **Staleness is per shard, not per index.** A run that refreshed Greenhouse
   invalidates the Greenhouse shards and nothing else.

### 6.2 Staleness is decided by the corpus's own hashes

Each shard's header carries the `FileMeta.SHA256` of the corpus file it was
built from. The corpus manifest carries the same hash, over **uncompressed**
bytes — the corpus design chose that deliberately so a Go upgrade that changes
the compressor does not invalidate content, and this design gets the benefit
for free.

At open, for each corpus file the query needs:

```
if index/<path>.jhti exists
   and its corpus_file_sha256 == manifest.Files[path].SHA256
   and its identity hash matches the manifest's
   and its format version is readable
then use the shard
else scan the corpus file
```

There is no third outcome. A shard built from yesterday's Greenhouse file has
yesterday's hash in its header, does not match, and is skipped. The user's
stated worry — getting lost in stale indexes — is answered by making a stale
index *unusable* rather than by making it detectable and hoping someone looks.

The header's `self_sha256` covers bit rot, and it is **not** verified on every
read: hashing the 92.9 MB image measures at 80 ms (1.17 GB/s), four times the
cost of the query it would protect. It is verified by `index build` and
`index verify`, and available at query time behind `--verify-index` for anyone
who wants to pay for it. This is a real gap stated plainly: a bit flip inside a
shard produces a wrong answer that the header check will not catch, `index
verify` is how you find it, and `index drop` is how you fix it.

### 6.3 A partially fresh index is a supported state

Because staleness is per shard and §5.4 proves a shard and a scan produce
identical results, a query may serve some files from the index and scan others
in the same execution. There is nothing to reconcile: both paths emit
`(sort key, row id)` pairs into the same bounded heap.

This is what makes the whole thing operationally boring. There is no rebuild
window during which queries are wrong, no "index is being rebuilt, please
wait", and no coordination between `corpus apply` and a reader. The worst a
half-built index can do is make a query slower, and `Result.Warnings` says so:

```
index: 138 of 153 shards fresh; scanned 15 corpus files directly (+1.9s)
```

### 6.4 Where it runs

- **`corpus apply` builds it by default** (`--index=false` to skip), because the
  rows are already decoded and the marginal cost is 1.2 s.
- **`index build` is the standalone path** for a corpus that arrived without
  one — downloaded from a release asset, or built by an older binary.
- **The nightly publishes it** alongside `open/`, gzipped, at 18.5 MB.
- **Nothing else needs it.** The crawler does not know it exists.

---

## 7. What is deliberately not indexed

The measured cost of a full image scan is 2–20 ms. Everything below is
something that would make some query faster than that and is not worth what it
costs.

### 7.1 No inverted or trigram index on titles

The strongest rejected option, and the numbers are real. A trigram index over
the 780,489 lowercased titles, built in the prototype:

```
built in 643ms: 7,350 distinct trigrams, 20,373,761 posting entries,
                81.6 MB raw (104.5 B/row)
  "security engineer"   1,418 candidates -> 1,393 matches in 1.1ms
  "engineer"           49,923 candidates -> 49,923 matches in 2.5ms
  "sre"                  217 candidates ->    217 matches in 0.1ms
  "go"                 cannot be served: term shorter than 3 bytes
```

It works. It is 5–15x faster than the arena scan on the queries it can serve.
It is rejected on four grounds:

- **It costs 104.5 B/row to save 15 ms.** That is 88% of the entire scan
  image's 119 B/row, to accelerate one of eleven predicates. Delta-encoding the
  posting lists would roughly halve it — that is an estimate, not a measurement
  — and 50 B/row to save 15 ms is still not a trade this project should make.
- **It cannot serve the queries people actually type.** `--title go` is two
  bytes and falls back to a full scan. So does `--title c`, `--title qa`,
  `--title ml`. A structure that is absent exactly when the query is hardest is
  not an index, it is a fast path.
- **It cannot serve `--exclude-title` at all.** Negation over a posting list is
  the complement of a set of 780,489, which is the scan.
- **It is a second thing that can be stale.** The scan image is already a
  derived cache with a hash and a fallback; adding a second one doubles the
  invalidation surface for a 15 ms win on a subset of one predicate.

Revisit if a measured workload shows title queries dominating *and* the arena
scan becoming the bottleneck. Neither is true at 20 ms.

### 7.2 No sorted index on `posted_at`, `first_seen` or pay

A B-tree on a date would let page 1 of `order=posted_at` skip the scan. It
would also have to be maintained, and it buys nothing for page 2 onward that
the bounded heap does not already give: §4.4 measures page 200 at the same cost
as page 1, and §2 measures all four orders at the same cost as each other. An
ordered structure is how you make paging flat when a scan is expensive. The
scan is 3 ms.

### 7.3 No index on company, source, platform, department, employment type or workplace type

Two reasons, and the first is the stronger.

**The corpus layout is already the index for the first three.** A source never
spans a file, files are platform-major, and the corpus manifest's `FirstKey`
and `LastKey` let a planner pick files by binary search. `--company anthropic`
reads one 523 KB file in 51 ms with no index in existence (§4.2). Building a
company index would be building a second copy of a decision the file layout
already made — the thing `docs/architecture-roadmap.md` warns about when it
says affinity is derived from `httpx`'s policy table rather than curated beside
it.

**Inside a shard, the dictionary already collapses them.** `--department
engineering` resolves against 131 distinct strings once, then costs one array
index per row: 3.4 ms for the whole corpus. Employment type and workplace type
are 3-bit and 2-bit codes; a bitmask test is not improvable.

### 7.4 No stemming, no synonyms, no relevance ranking, and no change to substring semantics

Substring matching stays (§1.1). Stemming would make `--title engineer` match
"engineering" — sometimes wanted, sometimes not, never predictable, and
untestable against a corpus whose titles come from 3,685 employers' house
styles. Synonyms would encode this project's opinion about another company's
job architecture, which `JobPosting.Seniority`'s doc comment already refuses to
do for levelling. Relevance ranking needs a score, and a score needs a total
order with a tiebreaker to be pageable at all, at which point it is a fifth
`Order` whose definition nobody can test.

The one real defect in substring matching — `--title go` matching "Cargo" and
"Chicago" — is worth fixing, and the fix is not an index. A word-boundary
variant is a check on the bytes either side of a match position in the arena
the scan is already reading:

```go
// wordMatch reports whether needle occurs in haystack at a word boundary.
// Costs one comparison per match, not per row, so it is free at the scale
// that matters: only rows that already matched pay for it.
func wordMatch(haystack, needle []byte) bool
```

Whether that becomes `--title-word`, a `word:` prefix on a term, or the default
with substring behind an opt-out is a product decision this document does not
make. It records that it costs no index and no bytes.

### 7.5 No description index

Descriptions are not in the corpus. `docs/design/corpus-format.md` deliberately
does not make full descriptions mandatory, and the roadmap lists job
descriptions among the values that must not become metric labels. Full-text
search over 780,489 descriptions is a different product with a different size
class, and it should be argued for on its own evidence rather than arriving as
a side effect of a title index.

---

## 8. Backends

```go
// Backend is what a surface attaches. The crawler never has one.
type Backend interface {
	Query(ctx context.Context, q Query, p Page) (Result, error)
	Stats(ctx context.Context) (Stats, error)
}

type Result struct {
	Postings []jobposting.JobPosting
	Rows     []Ref    // id, state, first_seen, last_seen — empty for live backends
	Next     Cursor   // zero when exhausted
	Total    int      // -1 unless Page.Count was set
	Scanned  int
	Facets   map[Dim][]FacetValue
	Generation int64  // corpus generation this page was answered from; 0 if live
	Complete bool     // false when a budget cut the answer short
	Warnings []string // "index: 138 of 153 shards fresh", "crawl truncated 2 sources"
}
```

| backend | pagination | index | notes |
| --- | --- | --- | --- |
| `live` (crawl) | **none**: `Next` is always zero, `Complete` reports truncation | n/a | Uses `Query.Match` over the crawl stream, exactly as today. Paging a live crawl would mean crawling again for page 2, so it is not offered rather than offered badly. |
| `memory` | full | n/a | Tests, and small one-shot results. |
| `corpus` | full | optional | §5, §6. The reference implementation and the definition of correct. |
| `remote` (HTTP) | full | server-side | `GET /postings?<Query.Values()>`; the cursor is the same opaque string. |
| `sqlite` (roadmap Phase 3) | full | its own | See the caveat below. |

The live backend's row is the one that matters for the roadmap's invariant that
the CLI works with no storage. `postings --title go --remote` does not touch
this interface at all: it builds a `query.Query`, calls `q.Apply` on the crawl
iterator, and streams. The `Backend` interface is what a *surface* attaches.

**One honest caveat about a future SQLite backend.** `LIKE` is not
`strings.Contains`: it is ASCII-case-insensitive in SQLite's default build and
does not agree with Go's Unicode `ToLower` on non-ASCII titles, of which a
3,685-source multinational registry has many. A SQLite backend must either
register a Go collation function or accept and document a divergence, and
`TestIndexAgreesWithScan` extended to that backend is what would catch it. That
is a reason to be slow about adding it, not a reason the interface is wrong.

---

## 9. Distribution, and an amendment to the corpus format

`docs/design/corpus-format.md` §3.1 reserves `index/slim-<nn>.jsonl.gz` for an
"optional derived search index" and measures it at **124 MB raw / 30.3 MB
gzipped (38.8 B/row)**.

This design proposes the scan image occupy that slot instead, on measured
grounds:

| | slim NDJSON | scan image |
| --- | ---: | ---: |
| published size | 30.3 MB gz | **18.5 MB gz** |
| local size | 124 MB | 92.9 MB |
| query cost | JSON decode: ~1–2 s est. | **2–20 ms** measured |
| predicates covered | company, title, location, platform, pay, remote | **all eleven** |
| positionally aligned with the corpus | no | yes — hydration is a block range |

It is smaller *and* carries department, team, employment type, workplace type,
exact dates and source identity, because a binary columnar layout does not
repeat key names or base-10 encode integers.

Sharding follows the corpus exactly — one `.jhti` per corpus file, per-shard
dictionaries, no global state — so:

- a browser after one company fetches one shard, tens of kilobytes;
- a browser after one platform fetches one directory;
- a browser doing a global text search fetches all of them, 18.5 MB gzipped,
  and caches them in IndexedDB across sessions;
- the CLI keeps the uncompressed image on disk, because decompressing 93 MB per
  query would cost more than the query.

The global-text-search case at 18.5 MB is a desktop-with-cache artifact, not a
phone-first one. That is the same honest weak point the corpus design already
records for its slim index, improved by 39% and not solved.

### 9.1 The browser build

Nothing in the image format touches mmap, file locking, `syscall` or a
filename. It needs `encoding/binary`, `bytes`, `sort`, `math` and `os.ReadFile`
— and in a browser, `os.ReadFile` is replaced by one `fetch` or one IndexedDB
read of the same bytes. Verified today: `GOOS=js GOARCH=wasm go build ./...`
and `GOOS=wasip1 GOARCH=wasm go build ./...` still succeed against the repo
unmodified, and the prototype builds and **runs** for `js/wasm` (§4.3) and
builds for `wasip1`.

No new module is proposed by this document. The whole query layer is
`bytes`, `context`, `encoding/base64`, `encoding/binary`, `errors`, `math`,
`net/url`, `slices`, `sort`, `strconv`, `strings` and `time`.

---

## 10. Rejected

**Bleve (`github.com/blevesearch/bleve/v2`).** The obvious Go answer for text
search, and it fails the project's hardest constraint outright. Measured today:
it pulls **53 modules**, and `GOOS=js GOARCH=wasm go build` fails on
`go.etcd.io/bbolt` — `undefined: syscall.Flock`, `undefined: unix.Mmap`,
`undefined: syscall.LOCK_EX`, `undefined array length maxMapSize`. It is the
`modernc.org/sqlite` case again: CGO-free is not the same as portable, and the
corpus has to be queryable in a browser.

**Roaring bitmaps (`github.com/RoaringBitmap/roaring/v2`).** Does build for
both wasm targets, which was worth checking. Rejected anyway because there is
nothing for it to do: the low-cardinality predicates already resolve to a
`[]bool` over a per-shard dictionary and cost one array index per row, and the
measurement in §4.3 shows predicate evaluation at 2.9 ms is not the bottleneck
in any query. A compressed bitmap library earns its place when set operations
dominate; here the arena does.

**SQLite as the query engine (`modernc.org/sqlite`).** Still the right answer
for roadmap Phase 3's local history store, still unable to be the query layer,
for the reason the corpus design already measured: it does not build for
`GOOS=js GOARCH=wasm` or `GOOS=wasip1`. A query layer that cannot run in the
browser forces every client-side surface behind a server, which is the outcome
`docs/surfaces-and-extensibility.md` exists to avoid.

**Offset pagination.** Breaks under concurrent writes in both directions — a
row inserted before the offset duplicates a row across pages, a row deleted
before it skips one — and gets slower with depth. §4.4 measures the keyset
alternative at constant cost to page 200. There is nothing to trade.

**A cursor that encodes a row number, a byte offset or a block index.** Any of
them would be invalidated by the compaction that the corpus design runs after
**every** run. The sort key plus the id survives it because a compaction moves
bytes and preserves identity.

**Snapshot isolation for a paging session.** Would eliminate the "rows added
mid-session may be missed" caveat in §3.3, at the cost of pinning a corpus
generation per open cursor and retaining generations nobody is reading. For a
job board where the corpus changes once a day and a paging session lasts
seconds, the caveat is smaller than the machinery.

**Indexing everything, i.e. a real database.** The honest version of this
rejection is that it was measured and lost: with the scan at 2–20 ms, every
index would be maintaining a structure to accelerate something already faster
than a network round trip, and every one of them is a thing that can be stale.

**Storing the display fields (URL, original-case title and location) in the
image.** Would eliminate the hydration step of §4.5 and roughly double the
artifact to ~205 B/row. Deferred rather than refused: hydration measures at
3.5–37.9 ms locally, which is acceptable now, and the case that would justify
it is a browser doing scattered range requests — which no browser has done yet.

**`Query` as an expression tree.** Users would get `(a OR b) AND NOT c`.
Backends would get an open-ended evaluation problem, five implementations of
it, and a class of query that a corpus scan can answer but a SQL backend
answers differently. §1.4's totality is worth more than the expressiveness.

**A `total_size` that is always computed.** Counting is a scan. It is free when
a scan is happening anyway and it is not free when a backend wants to stop
early, so it is a flag rather than a promise.

---

## 11. Open questions

- **Real cardinality is unmeasured.** Every dictionary number here comes from a
  44-company sample. The real registry has 3,462 companies and 3,685 sources,
  and the first real corpus should replace §5.5's dictionary row and confirm
  that dictionary resolution stays in the microseconds.
- **Whether the arena should be dictionary-encoded.** Locations repeat heavily
  — 4,916 distinct in 31,473 real postings — so a location dictionary could cut
  the arena by roughly 20 MB and make `--location` resolution nearly free. It
  cannot be measured on a replicated corpus without flattering itself, so it is
  deliberately not proposed. Measure it on the first real corpus.
- **Whether `--title go` should match at word boundaries by default.** §7.4
  says the fix is free; it does not say what the default should be. This needs
  a decision from whoever owns the CLI's compatibility surface, not a
  benchmark.
- **The browser has still never done this.** The js/wasm numbers are V8 under
  Node with a local file. A real PWA reads from IndexedDB or OPFS, over HTTP
  range requests, on a phone. `docs/surfaces-and-extensibility.md` already says
  the first task of client-side work is re-running its CORS table from an
  actual browser; the same applies to every number in §4.3's second table.
- **Facets are proposed without a consumer.** The TUI does not exist yet. If it
  turns out not to want them, they cost nothing to remove, which is the only
  reason they are in this document at all.
- **What a daemon does with cursors across a refresh.** `Result.Generation`
  reports the drift; nothing acts on it. A long-lived MCP session walking
  100,000 results across three runs is a case nobody has thought through.
- **Concurrency.** Every number here is single-threaded. The scan and the
  hydration both parallelise per shard and per block, and nothing above needs
  it, which is why nothing above does it.

---

## 12. Implementation order

Each step is useful on its own, and none of them changes existing output.

1. **Extract `query` from `internal/filter.go`.** `Query`, `Match`, `Apply`,
   `IsZero`, `Normalize`, `Values`, `Parse`, `Fields`. `internal.Filter` becomes
   an alias and `main.go` binds flags through `Fields()`. Pure refactor: the
   existing tests, including `TestFilterFieldsAreWiredIn`, must pass unchanged,
   plus a new round-trip property test that `Parse(q.Values(), t)` equals
   `q.Normalize()` for generated queries.
2. **Add `Order`, `Page`, `Cursor`, `Backend`, and the `memory` backend.**
   Nothing consumes them yet. This is where the cursor's fingerprint,
   fail-closed parsing and mutation behaviour get tested against a corpus small
   enough to enumerate.
3. **The `corpus` backend with no index.** File pruning from `Sources`,
   `Companies` and platform, block-range hydration, streaming scan. Ships
   `corpus query` with paging. At this point the flagship query costs 5 seconds
   and the tool is already useful for everything in §4.2.
4. **The scan image and `index build|verify|drop|stat`, with
   `TestIndexAgreesWithScan` written first.** The test is what makes the rest
   safe; write it against step 3's backend before there is an image to pass it.
5. **Build the image inside `corpus apply`,** incrementally, per dirty file.
   Measure the marginal cost against the 1.2 s the prototype projects at.
6. **Publish it,** gzipped, beside `open/`. Then, and only then, the TUI, the
   MCP tools and the PWA reader — all three consumers of an interface that has
   already been proven against a corpus the size of the real one.

Steps 1–3 add no artifact anyone has to maintain. Step 4 adds one, and it is
deletable.
