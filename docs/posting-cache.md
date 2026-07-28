# A posting cache for `postings`

**Status: design proposal for [#1](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/1). Nothing here is implemented.**

Each claim below carries the confidence markers
[docs/research/README.md](research/README.md) defines: `verified-in-code`,
`documented`, `inferred`, `speculative`. The distinction matters more than usual
here, because a cache that is wrong is indistinguishable from a crawl that is
right.

## What #1 asked for, and what changed under it

The issue, from 2019, proposes writing the results of a `postings` run to a
date-stamped file in `/tmp` and reading it back on the next run, with flags to
relocate, refresh, or disable it. It ends by asking whether this is worth the
complexity at all, given that `postings > file.txt` and `cat file.txt` already
work.

Both halves of that question have gotten sharper since, in opposite directions.

The case *for* is much stronger. In 2019 a crawl was "several minutes". The
07/26/26 baseline recorded 473,404 postings from 1,772 companies and **did not
finish in 350 minutes** on a GitHub runner (`verified-in-code`: quoted in
`shard_cmd.go:21-26` and `internal/enrich/README.md`). The registry is now 2,211
sources across 19 platforms. A tool nobody can afford to run twice is a tool
nobody runs once.

The case *against* is also stronger, and it is not complexity. It is that job
postings expire. Serving a day-old posting as though it were live sends someone
to a closed req. The 2019 sketch caches silently and says nothing about how old
the answer is, which is precisely the failure mode this project already refuses
elsewhere: pay figures carry `provenance` because "a wrong salary looks exactly
like a right one", and a partial crawl carries `partial` everywhere it goes.

So the design below is not "add a cache". It is **make time a first-class
property of a posting, and let a store of timestamped observations serve a query
when the caller says how stale is acceptable.** The speedup is a consequence.

## The thesis, in two claims

### 1. The unit of caching is a *source*, not a command

The crawl already narrows sources before fetching and applies every other filter
downstream of the stream (`verified-in-code`: `main.go:241` selects sources from
`--company`, `main.go:353`'s `postingFilterFor` deliberately drops the company
constraint, and `Filter.Apply` runs over the iterator). So the natural seam is
below the filters, at `services.Source`.

Key each entry by `platform + key` — the roadmap's stable integration ID, and
already what a posting carries in `internal.PostingSource`. Then:

- `postings --company anthropic` populates entries a later full crawl reuses;
- a full crawl populates entries every targeted query reuses;
- `--remote --title "security engineer"` and `--min-pay 200000` hit the same
  entries, because filters were never part of the key;
- there is exactly one identity in the system, and it is the one the manifest,
  the shard plan, and the enrichment table already use.

A command-level cache keyed on the flag set — what the issue sketches, and what
`postings > file.txt` effectively is — gets none of that. It cannot serve a
subset query from a superset run, it cannot refresh one board, and its key space
is the power set of twenty flags.

### 2. The record is an *observation*, not a copy of an answer

An entry means: *at time T, source S published exactly these postings.* That is
already the vocabulary of Phase 3 in
[architecture-roadmap.md](architecture-roadmap.md) — `posting_observation`
separate from `posting`, absence defined only from complete source runs. This
design is that model, one release early, in files instead of SQLite, behind an
interface that SQLite can implement later without the CLI noticing.

Framing it as observations rather than as a cache decides several arguments up
front:

| Question | Answer it forces |
| --- | --- |
| Is stale data OK? | The caller states a tolerance (`--max-age`); the tool never guesses. |
| What does a cache "miss" mean? | Nothing is stored fresh enough, so crawl it. Not an error, not a warning. |
| Can a partial fetch be stored? | No. An observation is a complete statement about a source or it does not exist. |
| Should output say where data came from? | Yes, always, on stderr — and in the record itself when asked. |

## Invariants this must not break

From [architecture-roadmap.md](architecture-roadmap.md), with what each one
implies for a store:

| Invariant | Implication |
| --- | --- |
| A partial crawl is never recorded or graphed as complete | The nightly writer (`total`) must not assemble a "complete" number from observations taken at different times. Default: `total` and `shard run` never read the store. |
| A failed source cannot make previously seen jobs look removed | Inverted here: a source that fails or is cut off mid-pagination must never leave a stored entry, or the next run reports a truncated list as that employer's whole board. Commit on completion only. |
| Source, company, and ATS identity are separate | Entries are keyed on `platform + key`. Never the company name — that conflation already caused a bug where `--company <tenant URL>` silently returned zero postings (`verified-in-code`: `main.go:344-357`). |
| Structured data to stdout, diagnostics to stderr | Cache hit/miss/age lines go to stderr. Stdout bytes are unchanged for a live crawl. |
| Secrets and high-cardinality values are not labels | Proxy URLs and credentials never appear in a path, an entry, or a log line. Metrics may carry platform and outcome; never a source key. |
| The default binary stays portable, no CGO | Standard library only: `os.UserCacheDir`, `compress/gzip`, `encoding/json`, `os.Rename`. No new module dependency. |
| Higher concurrency is not permission to increase pressure | A cache only ever *removes* requests. Nothing here raises a limit. |

## Design

### The store

```go
// Package cache stores what a job source published, and when.
package cache

// Entry is the metadata for one stored observation.
type Entry struct {
    SchemaVersion int                    `json:"schema_version"`
    Source        internal.PostingSource `json:"source"`
    Company       string                 `json:"company"`
    FetchedAt     time.Time              `json:"fetched_at"`
    Postings      int                    `json:"postings"`
    Bytes         int64                  `json:"bytes"`
    Commit        string                 `json:"commit,omitempty"` // diagnosis only
}

type Store interface {
    // Lookup reports what is stored for a source without reading its postings,
    // so a freshness decision costs one small file read.
    Lookup(internal.PostingSource) (Entry, bool, error)

    // Open streams a stored entry. The returned sequence is internal.Jobs, the
    // same type a live adapter returns.
    Open(internal.PostingSource) (Entry, internal.Jobs, error)

    // Create returns a writer whose output becomes visible only on Commit.
    Create(internal.PostingSource) (EntryWriter, error)

    Prune(PruneOptions) (PruneResult, error)
}

type EntryWriter interface {
    Write(*internal.JobPosting) error
    Commit(Entry) error // atomic publish
    Close() error       // discard unless committed
}
```

`Open` returning `internal.Jobs` is the whole composability argument in one line.
A stored source and a live source are the same type, so `Dedupe`, `Filter.Apply`,
enrichment, the CSV/JSON printers, `shard.PostingWriter`, and `services.Observe`
are all unchanged and untouched.

`Write`/`Commit` rather than "hand me a slice" is not fussiness: FedEx alone
publishes over 138,000 postings (`verified-in-code`: `main.go` health limit
comment). Nothing in this design may hold a source's output in memory.

### On-disk layout

```
$XDG_CACHE_HOME/job-hunter-toolkit/      # os.UserCacheDir(); $JHT_CACHE_DIR; --cache-dir
  v1/
    a3/a3f19c…4b.ndjson.gz               # full JobPosting records, one per line
    a3/a3f19c…4b.json                    # the Entry above
  trim.txt                               # last prune time, Go-build-cache style
```

- **Entry id** is a hash of `schema version + platform + key`, fanned out over
  256 subdirectories. 2,211 entries today; the fanout is what keeps this pleasant
  at ten thousand (`documented`: the same shape Go's build cache uses).
- **NDJSON** is already this project's stream format — `postings --json` emits
  it, `shard run` writes it. One format, readable with `jq`, appendable,
  streamable.
- **gzip from the standard library**, not zstd: no new dependency, no CGO.
  Posting JSON should compress roughly 5–8× (`inferred`; verify with
  `postings --company <big> --json | gzip -c | wc -c` before committing to it).
  A full crawl is on the order of 150–250 MB raw (`inferred` from ~473k postings
  at ~350 bytes) — a size worth compressing on a laptop.
- **Data file renamed before metadata.** A crash between the two leaves an
  orphan data file with no `Entry`, which reads as a miss. The reverse order
  would leave metadata pointing at a half-written stream, which reads as a lie.
  Orphans are collected by `Prune`.
- Atomic publish is `os.CreateTemp` + `os.Rename`, the pattern
  `writeJSONAtomic` already uses (`verified-in-code`: `internal/shard/plan.go:507`).
  Concurrent invocations are safe by construction: last rename wins, readers
  never see a torn file, and no lock file is needed.

### The decorator, and where it sits

```go
// Sources wraps sources so each reads from the store when a stored observation
// is fresh enough, and writes through when it crawls.
func Sources(store Store, policy Policy, sources []services.Source) ([]services.Source, func() Stats)
```

This is deliberately the same shape as `services.Observe`
(`verified-in-code`: `internal/services/observe.go:48`), which already turns
`[]Source` into wrapped work plus a result snapshot. Composition:

```go
sources          := services.SourcesMatching(filter.Companies)
cached, cacheStats := cache.Sources(store, policy, sources)
sourceJobs, runs   := services.Observe(cached, logger)
jobs               := internal.Dedupe(internal.AllWithConcurrency(ctx, client, n, sourceJobs...))
```

The cache goes **inside** `Observe`, not outside, so a served source still
appears in the manifest with its identity, count, and outcome. A cache that
made a thousand sources vanish from the manifest would turn the project's only
crawl diagnostics into a lie about coverage.

That choice has one consequence that must land in the same change (see
[Interactions](#interactions-with-code-that-already-exists)).

### Commit-on-complete

An entry is published only when a source's iterator is **exhausted with zero
errors**. `Observe` already tracks exactly this distinction — `exhausted`,
`ctx.Err()`, `run.Errors` — and classifies the result as `complete`, `failed`,
`truncated`, or `stopped` (`verified-in-code`: `observe.go:105-126`). The store
publishes for `complete` and discards otherwise:

- a source cut off by the crawl deadline stores nothing;
- a source whose 5th page 404s stores nothing, so the next run cannot report
  four pages as that employer's entire board;
- a consumer that stops early (`| head -20`, a broken pipe, an error downstream)
  stores nothing, because `yield` returning false is not evidence about the
  board.

A source that completes with **zero** postings *is* stored: "reachable and not
hiring" is a real observation, and it is what `health` already calls `empty`
rather than `failed`. The cost is that a board which 200s with an empty list
because its tenant was renamed stays wrong for the tolerance window. That
misdiagnosis exists today; caching extends it by at most `--max-age`, and
`health` remains the tool for it.

### Freshness is a filter, not a mode

There is no cache on/off switch. There is a tolerance:

```
--max-age 24h    # use observations younger than this; crawl everything else
--max-age 0      # crawl everything (what --refresh means)
```

Two details the 2019 sketch gets wrong, both worth stating because they look
like nitpicks and are not:

1. **Not a date-stamped filename.** A crawl finishing at 23:59 must not expire
   at 00:00. Freshness is `now - FetchedAt < MaxAge`, per entry.
2. **Per entry, not per run.** A run mixes ages — that is the point. Yesterday's
   Greenhouse and today's freshly-added Workday tenant coexist, and the run
   reports its oldest observation rather than pretending to an instant.

`--max-age` should accept the same spellings `--posted-since` does — `7d`, `2w`,
`72h` — by reusing `parseAge` (`verified-in-code`: `main.go:741`). A tool where
`--posted-since 7d` works and `--max-age 7d` errors is a tool that taught its
user a rule and then broke it.

### CLI surface

| Flag | Default | Effect |
| --- | --- | --- |
| `--max-age` | `24h` on `postings`; `0` elsewhere | serve observations younger than this; crawl the rest |
| `--refresh` | off | sugar for `--max-age 0`; still writes through |
| `--no-cache` | off | read nothing, write nothing, touch no disk |
| `--cache-dir` | `os.UserCacheDir()/job-hunter-toolkit`, or `$JHT_CACHE_DIR` | where entries live |

```
job-hunter-toolkit cache path                       # print the directory
job-hunter-toolkit cache stats [--json]             # entries, bytes, oldest, per platform
job-hunter-toolkit cache prune [--older-than 7d] [--max-size 2GiB]
job-hunter-toolkit cache clear
```

Precedent for the command group: `go clean -cache`, `gh cache`, `npm cache
verify` (`documented`). Precedent for pruning on a timer rather than on every
write: Go's build cache trims entries unused for five days, at most once a day,
recorded in a marker file (`documented`) — adopt the same, so a full crawl never
pays for a directory walk and a user's home directory never grows without
bound.

**Provenance goes to stderr on every run that served anything:**

```
$ job-hunter-toolkit postings --remote --title "security engineer" --stats
...
1,204 of 1,772 sources from cache (oldest observation 19h ago), 568 crawled
2,391 postings from 1,772 sources (0 sources failed)
```

Silence here would be the actual bug. The user needs to know they are reading
yesterday.

### Provenance in the data, not just on stderr

`internal.PostingSource` gains one field:

```go
// ObservedAt is when this posting was fetched from the board. It is zero, and
// therefore absent, on a live crawl: the answer is "now". It is set only when a
// posting came from the store, which is the one case where a consumer cannot
// otherwise tell how old the claim is.
ObservedAt time.Time `json:"observed_at,omitzero"`
```

This is the same rule `Compensation.Provenance` follows, applied to time. Note
what it does *not* do: a live crawl's JSON is byte-identical to today's, because
`omitzero` drops a zero timestamp — consistent with how the rest of
`JobPosting` treats absence (`verified-in-code`: `internal/job_posting.go:9-21`).
The frozen 8-column CSV set is untouched; `observed_at` is appended to
`--csv-columns extended`, which is exactly what that opt-in set exists for
(`verified-in-code`: `main.go:384-400`).

### Where this must **not** be wired

`total` and `shard run` default to `--max-age 0`: they never read the store.

The daily record is a coverage-and-health time series before it is a hiring time
series ([jobs-record.md](jobs-record.md)), and a row assembled from observations
taken across a day is not a measurement of anything at an instant. It would also
quietly repair the exact symptom — a crawl that cannot finish in 350 minutes —
that the sharded workflow exists to solve honestly.

If that ever changes, it changes with: a manifest field recording the oldest
observation, a fourth-column status that is not the string `complete`, and a
matching guard in `shard merge`. Not before.

A narrower and defensible version does exist: a **retried** shard skipping the
sources it already finished in the failed attempt. It needs the observations to
survive between ephemeral runners (`actions/cache`), and it needs the merge to
reason about mixed observation ages. It is out of scope here and should not be
attempted until the sharded workflow has run against a live board at all
(`verified-in-code`: [architecture-roadmap.md](architecture-roadmap.md) says it
never has).

### Correctness guards on an entry

- **`schema_version`** is bumped whenever a change alters what an adapter
  *parses*, not merely how it fetches. An entry from another version is a miss,
  not an error — Go's build cache behaviour, and the only behaviour that makes
  format evolution survivable. This belongs on the checklist in
  [adding-a-source.md](adding-a-source.md): *"did you change what a posting
  means? bump the cache schema."* A pay-parsing fix that leaves month-old wrong
  pay in a store is a defect the fix appears to have addressed.
- **`commit` is recorded but never keyed on.** Keying on the binary's VCS
  revision would be safe and useless: every `go install` would discard
  everything. Recording it makes "which build wrote this nonsense?" answerable.
- **A corrupt or truncated entry is a miss.** Decode errors discard the entry
  and crawl. The store is an optimisation; it never fails a run. (Contrast
  `shard merge`, which fails closed — because there, a missing shard is missing
  data, while here it is only a slower answer.)

## Security and privacy: why not `/tmp`

The 2019 sketch's `/tmp/job-hunter-toolkit-cache-file-with-date-format` is a
predictable path in a world-writable directory. That is a real problem here, not
a theoretical one, and it is worth being blunt about why: **the payload is URLs
a job hunter will click and send a résumé to.** Cache poisoning on this data is
a phishing primitive.

- On a shared host any local user can pre-create that path, or a symlink at it,
  before the tool does. Writes land wherever the attacker points; reads return
  whatever the attacker wrote.
- `/tmp` is cleared on reboot on most systems, which defeats the feature.
- One path, several users, one collision.

Instead:

- `os.UserCacheDir()` — `$XDG_CACHE_HOME` or `~/.cache` on Unix,
  `~/Library/Caches` on macOS, `%LocalAppData%` on Windows (`documented`: Go
  standard library). One line, correct on three platforms, and where users
  already expect to find and delete cached data.
- Directory `0700`, files `0600`. The set of entries reveals which employers
  someone has been looking at; a job hunt is private.
- Refuse a cache directory that is not owned by the current user, or that is
  group- or world-writable, the way `ssh` and `git` refuse comparable paths.
  Open entries with `O_NOFOLLOW` where the platform has it.
- Nothing in the store is ever executed, and no credential — proxy or otherwise
  — is ever written into a path or an entry.

## Interactions with code that already exists

These are the places a first implementation will break something if it is not
looking. All `verified-in-code`.

1. **`internal/shard/cost.go:57` — `EstimateCosts` would poison the shard plan.**
   It takes the median `duration_ms` per source over prior manifests. A cached
   source finishes in milliseconds. Let those samples in and the planner learns
   that FedEx costs 3 ms and packs it alongside six hundred other sources. Fix:
   `services.SourceRun` gains `Cached bool` (or `Origin string`), and cached
   runs are excluded from cost sampling exactly as `planned` and `running`
   already are (`cost.go:39-44`). This must land in the same change as the
   decorator, not after.
2. **`services.SourceRun` and the manifest** gain that field, which means schema
   version 3 — additive, so the nightly workflow's Python summariser keeps
   working (`internal/shard/manifest.go:13-23` documents why that matters).
3. **`internal/shard/postings.go`** deliberately stores identity only, three
   fields per posting, because the merge needs two numbers. The cache stores
   whole postings. They are different artifacts for different jobs and should
   not be unified; `DedupeIdentity` stays the single shared definition of
   posting identity.
4. **`--stats`** grows the cache line above; `crawl_report.go` grows the
   manifest counts.
5. **`slog` events** `cache.hit` / `cache.miss` / `cache.write` / `cache.prune`
   slot into the roadmap's existing hierarchy under `source.start`. Attributes:
   platform, outcome, age. Never the source key in a metric label.

## What it is actually worth

`inferred` throughout — no numbers here were measured, and the environment this
was written in has no outbound access to a job board.

| Scenario | Today | With a 24h tolerance |
| --- | --- | --- |
| `postings --company anthropic` | ~1 s | ~1 s (nothing to win) |
| `postings --remote --title X`, full crawl | 350+ min, incomplete | seconds, from disk |
| Iterating on a filter or an output format | one crawl per attempt | one crawl, then free |
| Cold run on a new machine | 350+ min | 350+ min, unchanged |

The middle two are the whole feature. The last row is worth stating plainly:
**this makes the second run fast and does nothing at all for the first.** Every
coverage and wall-time problem in
[research/crawl-performance.md](research/crawl-performance.md) survives this
design untouched, and a cache must never become the reason they stop getting
fixed.

There is a second, quieter benefit. A store of observations is the prerequisite
for the agent-facing tools in Phase 4: `search_jobs` must be *bounded*, and
"answer from observations no older than 24 hours" is the only bound that is both
fast and honest. Without it, the only two options are a crawl per conversational
turn or a lie.

## Layer B: HTTP revalidation, and why it is probably not worth it

The obvious follow-on is an RFC 9111-shaped cache in `internal/httpx` —
`If-None-Match` / `If-Modified-Since`, so refreshing a source costs a 304
instead of a full response. It is the textbook answer, and the transport is the
right layer for it (an adapter paginates N times; the adapters must not each
learn conditional requests). Note the research before building it:

- Greenhouse's public board API is documented as unauthenticated, cached, not
  rate limited, with a recommendation to *"cache aggressively"* and poll each
  board every few hours (`documented`, secondary sources —
  [Greenhouse API overview](https://support.greenhouse.io/hc/en-us/articles/10568627186203-Greenhouse-API-overview),
  [JobsPipe's reference](https://jobspipe.dev/guides/greenhouse-jobs-api)).
  Whether it emits `ETag` or `Last-Modified` is **not documented anywhere I
  could find**.
- Ashby's public posting API exposes no rate-limit headers and no documented
  cache validators (`documented`, secondary —
  [Ashby's public job posting API](https://developers.ashbyhq.com/docs/public-job-posting-api),
  [Ashby Jobs API notes](https://fantastic.jobs/ats/ashby)).
- No adapter in this repo sends a conditional header today, and `fetchJSON`
  treats any non-200 as a failure, so a 304 would currently be an error
  (`verified-in-code`: `internal/services/json.go:45-88`).

And the honest arithmetic: a 304 still costs a *request*, and this crawl is
bounded by per-service concurrency and request count, not by bytes
(`research/crawl-performance.md`). Revalidating 2,211 sources across their
pagination saves parsing and bandwidth while saving approximately none of the
wall time that made #1 worth filing. **The source-level store removes the
requests entirely; Layer B only makes them cheaper.**

Recommendation: do not build it. If it is ever revisited, the first step is
measurement, not code, and it is cheap — a `health` run that records which
platforms return `ETag`, `Last-Modified`, or `Cache-Control`, and whether
`If-None-Match` actually yields a 304. Only GitHub Actions can run it. That is
the same rule [adding-a-source.md](adding-a-source.md) already sets: capture one
real response before modelling a platform.

## Rejected alternatives

| Alternative | Why not |
| --- | --- |
| Cache the command's output, keyed on the flag set (the 2019 sketch) | Cannot serve a subset query from a superset run, cannot refresh one board, and the key space is the power set of the flags. |
| `postings --json > file` and re-read it (the issue's own "why not") | Works, and is genuinely enough for one person with one query. It cannot express partial refresh, cannot mix ages, and gives the tool no way to tell the user how old the answer is. |
| Start at Phase 3 with SQLite | Right destination, wrong first step. The interface above is what makes it a later implementation detail rather than a rewrite; files first means no schema migration story is needed to answer #1. |
| Key entries on the binary's commit | Safe and useless: every `go install` discards everything. |
| Cache at the HTTP layer only | Saves bytes, not requests, and requests are the constraint. See Layer B. |
| A daemon that refreshes in the background | The roadmap is explicit that a daemon must never be the only way to get history, and #1 does not need one. |
| Name the package `snapshot` | The roadmap reserves that word for a complete crawl artifact. This is a store of per-source observations, which is a different thing. |

## Delivery sequence

Each step is independently reviewable and leaves the tree working.

1. **`internal/cache`**: `Store`, the file implementation, the entry format, and
   hermetic tests. No CLI change. Tests use `t.TempDir()` and an injected clock
   (`Policy.Now`), so nothing sleeps and nothing touches a network — the suite
   still runs in under a second. Cases that matter: round-trip equality; a
   stopped iterator commits nothing; a failed source commits nothing; a
   truncated file is a miss, not an error; concurrent writers both succeed;
   prune removes what it should and orphans too.
2. **Wire it into `postings` only, defaulting to `--max-age 0`.** The mechanism
   ships inert and opt-in. This is how `track_jobs_sharded.yml` shipped —
   dispatch-only, `record: false` — and for the same reason: a change that
   cannot be observed before it is trusted has to be trusted before it is
   observed.
3. **`SourceRun.Cached`, manifest schema 3, and the `EstimateCosts` exclusion**,
   together, plus the `--stats` and `slog` lines.
4. **The `cache` command group and pruning.**
5. **Measure and publish**: cold versus warm wall time, store size on disk,
   compression ratio, and read throughput on a full crawl. Numbers go in this
   document.
6. **Flip the default to `--max-age 24h` on `postings`** — a separate change,
   justified by step 5, and the only step that alters what an existing command
   does.
7. Only then reconsider Phase 3 storage or Layer B.

## Open questions for the maintainer

1. **Should the default really be on?** Step 6 is the only genuinely
   contentious change here. On means `postings` stops being a live-search
   command by default; off means most users never get the feature. The
   recommendation is on-with-a-visible-age-line, but it is a product call.
2. **Is 24 hours the right tolerance**, and should it vary by platform?
   Greenhouse's own guidance is "every few hours" — a per-platform default table
   is possible, but it is a second table to keep true, and this project already
   prefers deriving from one (`internal/shard/affinity.go` asks the rate limiter
   which backend a source uses rather than maintaining a parallel list).
3. **Should `observed_at` appear in JSON output?** It makes cached output
   differ from live output. That is the intent — but it is a change to a
   documented data contract, and it deserves an explicit yes.
4. **May `total` ever read the store?** The recommendation is no. If the answer
   is "yes, for retries", the guard rails in
   [Where this must not be wired](#where-this-must-not-be-wired) are the
   minimum price.

## Provenance

Written 2026-07-28 against commit `b971a11`, by reading this repository,
`docs/architecture-roadmap.md`, `docs/research/crawl-performance.md`, and vendor
documentation for the Greenhouse and Ashby public board APIs. Nothing was
verified against a live job board: the environment had no outbound access to
one. Every performance figure is inferred or quoted from an existing document,
and every figure in [What it is actually worth](#what-it-is-actually-worth)
should be replaced with a measurement before this design is used to justify
anything.
