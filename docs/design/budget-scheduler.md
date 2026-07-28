# The budget-aware scheduler

`docs/crawl-budget-model.md` decides that a run is a bounded amount of work
against a corpus rather than an attempt at a complete pass. It specifies the
model and names three properties the mechanism must have — bounded and
interruptible, fair, deterministic. This document specifies the mechanism: the
record persisted per source, the function that turns that record plus a budget
into an ordered work list, the executor that stops cleanly, and how all of it
composes with `internal/shard` rather than duplicating it.

Everything numbered below was measured on 2026-07-28 in a 4-vCPU container with
Go 1.26.5, against the real source registry in this tree and the real affinity
keys `internal/shard` derives from `internal/httpx`. Prototypes are in the
scratchpad, not the repo, and are cited where their numbers are used. Costs in
the multi-run simulations are **synthetic**, shaped by the per-platform means in
`docs/measurements/2026-07-28-crawl.md`; they model scheduler behaviour, not
crawl wall time, and every table says which.

---

## 0. What changed under this design while it was being written

The measurement doc reports 3,685 sources. This working tree has **8,145**,
across 22 platforms — other work in flight has added Breezy (1,492), Teamtailor
(988), and grown Oracle Cloud to 1,203 and SuccessFactors to 717
(`scratchpad/proto/tools/groups`). That is not a footnote. It changes the
premise:

| affinity group | sources | limiter slots | sequential rounds |
| --- | ---: | ---: | ---: |
| `platform:breezy` | 1,492 | 4 | 373 |
| `platform:teamtailor` | 988 | 4 | 247 |
| `platform:personio` | 970 | 4 | 243 |
| `platform:greenhouse` | 647 | 4 | 162 |
| `platform:recruitee` | 492 | 4 | 123 |
| `platform:ashby` | 418 | 4 | 105 |
| `service:career5.successfactors.eu` | 280 | **2** | 140 |

8,145 sources collapse to **1,003 affinity groups**, of which 833 are
singletons and 14 hold 100 or more sources. Personio is no longer the only
243-round platform; it is one of four. The scheduler cannot fix any of them —
see §7 — but the case for spending a budget by measured cost rather than by
source count is now four times stronger than the measurement that motivated it.

---

## 1. The shape of the answer

Three functions and one file.

```
        sources.jsonl ──────┐
                            ▼
  registry ──▶ schedule.Build(sources, store, opts) ──▶ Plan (ordered work list)
                            │                            │
                            │                            ├─▶ shard.Build(...)   [optional]
                            │                            ▼
                            │                     execute with a dispatch gate
                            │                            │
                            │                            ▼
                            └──── schedule.Fold(store, manifest) ◀── manifest.json
                                            │
                                            ▼
                                     sources.jsonl
```

`Build` is a pure function. `Fold` is a pure function. The only impure parts are
reading and writing one file, and the crawl itself, which already exists.

Package: **`internal/schedule`**. It is policy, not vocabulary, so it stays
internal — `docs/surfaces-and-extensibility.md` says nothing goes public without
a consumer. It imports `internal/services` and `internal/shard` (for
`AffinityKeys`, `Manifest`, `SourceRef`); nothing imports it except `internal/cli`
and the tests. If `docs/design/package-taxonomy.md` lands, `schedule` reads
`snapshot.Manifest` instead of `shard.Manifest` and is otherwise unchanged.

**Zero new modules.** The prototype uses `crypto/sha256`, `encoding/hex`,
`encoding/json`, `slices`, `strings`, `time`. Verified building for `js/wasm`,
`wasip1/wasm`, `linux/arm64`, `darwin/arm64` and `windows/amd64` with
`CGO_ENABLED=0`; `go list -m all` is unchanged at 16 entries.

---

## 2. Cost is per backend, not per run — the fact that shapes everything else

A scheduler that treats a wall-clock budget as a pool of source-seconds is
wrong, and wrong in the direction that wastes most of the budget.

Take the numbers from the 07/28 run: 8,362 source-seconds against 720 seconds of
wall clock at concurrency 24 — 48% utilisation. Personio alone was 2,484
source-seconds through a 4-slot limiter key, which is 621 seconds of *group*
time, and the run took 720. **The critical path was one backend, not the worker
pool.** Sources in different affinity groups do not contend; sources in the same
one do, at exactly the concurrency `httpx` grants that key.

So the budget arithmetic is two constraints, not one:

```
per group g:   Σ predicted_cost(s)  ≤  budget × parallelism(g) × fill
globally:      Σ predicted_cost(s)  ≤  budget × workers × shards × fill
```

Measured difference between the two accounting models, on the 07/28-shaped
registry at 24 workers and a 12-minute budget, 20 simulated runs
(`scratchpad/proto/tools/schedsim`):

| budget accounting | sources admitted/run | source-seconds/run | wall used |
| --- | ---: | ---: | ---: |
| flat sum of durations | 2,469 | 6,485 | **38% of budget** |
| per-group critical path | **3,120** | **9,196** | 90% of budget |

Flat-sum accounting leaves 62% of the budget on the floor and does 29% less
work, because it charges a group's whole 4-way-parallel time against a
single-threaded clock. This is the single most consequential decision in the
design and it is why `parallelism(g)` has to be a real number rather than a
guess.

### Where `parallelism(g)` comes from: measured, never curated

`httpx.ServicePolicyForHost` knows a backend's slot count — but only given a
*hostname*, and the planner runs offline. For 833 of 1,003 groups
`shard.AffinityKeys` already resolves a host and the answer is exact. For the
big platform-keyed groups (Breezy, Teamtailor, Personio, Greenhouse, …) the ATS
key is a board slug and no host is derivable without a request.

Rather than adding a second table to drift from `httpx` — the mistake
`docs/surfaces-and-extensibility.md` §4 explicitly rules out — derive it from
the manifest that already exists:

```
parallelism(g) = max over prior runs of  Σ duration(s in g) / (max finish − min start)
```

`services.SourceRun` already carries `StartedAt` and `FinishedAt` per source, so
this needs no new field. Three properties make `max` the right estimator rather
than a median:

- It is **mathematically a lower bound** on the concurrency actually achieved:
  total busy time over a span cannot exceed slots × span. It can never
  over-estimate.
- A run that scheduled only two sources of a group under-estimates that group;
  taking the max across runs lets a later, busier run correct it.
- It absorbs everything the semaphore count would miss — the 25 ms pacing
  interval, 429 cooldowns, retry sleeps — because it measures achieved
  throughput, not permission.

Cold start (no prior run for a group) uses `httpx.DefaultPerHostLimit`, which is
4. That is derived from `httpx`, not invented, and it is the modal value in the
limiter table. Under-estimating parallelism under-admits, which finishes early;
over-estimating over-admits, which the dispatch gate in §5 absorbs.

`fill` defaults to 90%. The last source admitted to a group should finish before
the deadline: a source truncated by our own deadline costs its full duration and
cannot advance `last_seen`, so it is worse than not starting it.

---

## 3. The state record

### 3.1 The record

```go
package schedule

// SourceID is the stable integration identity: platform + ATS key, the same
// pair shard.SourceRef and services.SourceRun already use. Company is display
// text and is never part of identity.
type SourceID struct{ Platform, Key string }

// SourceState is what one source carries between runs. Roughly 190 bytes.
type SourceState struct {
    Platform string `json:"platform"`
    Key      string `json:"key"`
    Company  string `json:"company,omitempty"`

    // Group is the affinity key this source was last planned under. It is a
    // cache of shard.AffinityKeys, recorded so `schedule status` can explain a
    // plan without recomputing the registry, and recomputed (never trusted) by
    // Build.
    Group string `json:"group,omitempty"`

    // LastAttempt advances whenever the crawler actually started this source.
    // LastSuccess advances only on a `complete` run. Staleness is measured from
    // LastSuccess; back-off is measured from LastAttempt. Conflating the two is
    // a bug this design found by testing for it (§8).
    LastAttempt string `json:"last_attempt,omitempty"` // RFC3339 UTC
    LastSuccess string `json:"last_success,omitempty"`

    // Trailing samples, oldest first, capped at 7. The median is the estimate;
    // a median over a week already absorbs one anomalous day, which is the
    // argument internal/shard/cost.go makes for its own estimator and there is
    // no reason to make a second one.
    DurationMS []int32 `json:"duration_ms,omitempty"`
    Postings   []int32 `json:"postings,omitempty"`

    ConsecutiveFailures int32  `json:"consecutive_failures,omitempty"`
    ErrorClass          string `json:"error_class,omitempty"`

    // Retired marks a source that left the registry. Its record is kept so a
    // temporarily removed adapter does not lose its history; a retired record
    // is dropped after RetireAfter (90 days).
    Retired   bool   `json:"retired,omitempty"`
    RetiredAt string `json:"retired_at,omitempty"`
}

// GroupState is one affinity group's measured capacity (§2).
type GroupState struct {
    Key              string `json:"key"`
    ParallelismMilli int32  `json:"parallelism_milli"` // 4000 == 4 concurrent
    ObservedAt       string `json:"observed_at,omitempty"`
    Samples          int32  `json:"samples,omitempty"`
}
```

Everything here except `Group`, `ParallelismMilli` and `RetiredAt` is already
emitted by `total --manifest` today. This is deliberately the smallest possible
delta on the existing manifest, and it is what
`docs/crawl-budget-model.md` means by "the inputs exist; nothing consumes them
across runs."

### 3.2 The file

One file, JSON Lines, three record kinds discriminated by `kind`:

```
{"kind":"header","schema":"job-hunter-toolkit/scheduler-state","version":1,"written_at":"2026-07-28T09:12:00Z","writer":"9c71fc0"}
{"kind":"source","platform":"ashby","key":"0x","company":"0x","last_attempt":"2026-07-28T09:03:11Z","last_success":"2026-07-28T09:03:11Z","duration_ms":[80,74,91],"postings":[13,13,14]}
…
{"kind":"group","key":"service:boards-api.greenhouse.io","parallelism_milli":3820,"samples":6}
```

Ordering is header, then sources sorted by `(platform, key)`, then groups sorted
by `key`. Field order follows struct order. `json.Encoder` with
`SetEscapeHTML(false)`. Same fold inputs therefore produce byte-identical
output, which is what makes the file reviewable in a diff and safe to hash.

**Measured** (`scratchpad/sources.jsonl`, 8,145 real source keys after 14
simulated runs): **1.52 MiB raw, 179 KiB gzipped**, ~190 B/row. RFC3339
timestamps rather than the prototype's Unix integers add roughly 16 B/row, so
budget ~1.65 MiB / ~190 KiB. Well under the megabyte the budget model guessed at
3,685 sources, and still trivial at 8,145.

Why JSONL and not the corpus's columnar file: it is read whole, written whole,
and reviewed by humans. `docs/design/storage-engine.md` reaches the same
conclusion for the same file ("`sources.json` and `runs.ndjson` are deliberately
*not* columnar … being reviewable in a diff is worth more than being fast"), and
`docs/design/corpus-format.md` §3.5 already carries a superset of these fields
as `sources.jsonl.gz`. **Where a corpus exists, its source-state file is the
store and this format is its projection; the scheduler defines the fields it
needs and does not own the file.** `schedule.Store` is an in-memory struct with
a `Decode`/`Encode` pair, and a corpus adapter that fills the same struct from
`corpus.SourceState` is fifteen lines. There must not be two state files.

### 3.3 Where it lives, per surface

| surface | store | writer | notes |
| --- | --- | --- | --- |
| Actions nightly | release asset `scheduler-state.jsonl.gz` on a `state` tag | the crawl job | `actions/cache` as a fast path keyed by state digest; a cache miss falls through to the release asset |
| Sharded Actions run | same asset | **the merge job only** | shards write manifests; the merge folds all of them once. N shards must never be N writers |
| CLI one-shot | `$XDG_STATE_HOME/job-hunter-toolkit/scheduler-state.jsonl` | the command, if `--state` was given | absent by default; `total` with no `--state` behaves exactly as today |
| Daemon | the same path, single process | itself | folds after each source instead of at the end |
| Browser / PWA | IndexedDB: object store `source_state` keyed `platform\x00key`, `group_state` keyed by affinity key | the tab | seeded from the published file, **never merged back** |
| Service worker | the same IndexedDB | itself | tiny budget; the plan mostly orders rather than selects |

Two rules that fall out of that table:

- **State is advisory. Losing it is never an error.** A missing, unreadable,
  truncated or future-versioned file degrades to cold start: every source is
  maximally stale, costs are unknown, and the plan becomes an ordered full pass
  bounded by the budget — which is today's behaviour. The run logs a warning and
  stamps `state_source: "cold"` in its manifest. Refusing to crawl because
  yesterday's artifact expired is the failure mode the sharded work already
  guards against in `loadPriorCosts`.
- **Per-surface state is private.** A duration measured on a GitHub runner does
  not predict a phone's, and a browser blocked by CORS must not record a source
  as failed into shared state. A surface seeded from the published file
  **discards `duration_ms` and re-learns it**, and keeps `last_success`,
  `postings` and `consecutive_failures` as a prior. Cost is a property of the
  machine and the network; the rest is a property of the board.

Not committed to git: at 179 KiB changing daily, a committed state file is
roughly 65 MB of git objects a year in every clone and every CI checkout, which
is item 5 of the budget model's own cost list.

### 3.4 Surviving a schema change

1. **A missing record is well defined**, and it is the cold-start path — which
   is optimistic by construction (§4.3). That is the whole migration story:
   any change that drops records costs one un-prioritised run, not correctness.
   Every other rule exists to keep it that way.
2. **Additive fields never bump the version.** Readers ignore unknown fields;
   `DisallowUnknownFields` must never be set on this file.
3. **Unknown `kind` lines are skipped with a warning, and dropped on rewrite.**
   Preserving them verbatim would conflict with deterministic sorted output. The
   cost is bounded by rule 1.
4. **A field's meaning never changes.** Changing what a field means means a new
   field name; the old one is left in place, unread, until a version bump
   removes it.
5. **A higher `version` than the reader knows is a cold start, not an error.**
   An older binary re-running against a newer state file must degrade, not fail
   — the nightly is the only live verification this project has.

---

## 4. Scoring

Two lanes, one greedy admission pass, integer arithmetic throughout.

### 4.1 The terms

For source *s* at quantised time *now* (§6):

```
cost(s)      = median(DurationMS)                     clamped to [1 ms, 30 min]
               ↳ else platform median  ↳ else global median  ↳ else 1000 ms

yield(s)     = median(Postings)
               ↳ else platform p75     ↳ else global p75     ↳ else 1

age(s)       = now − LastSuccess       (never succeeded ⇒ +∞)
target(s)    = Policy.Target, or Policy.PerPlatform[platform]     default 24 h

stale(s)     = min(1000 × age(s) / target(s), 4000)   milli-units, capped at 4×

score(s)     = yield(s) × stale(s) × 1000 / cost(s)   int64
```

`score` is postings-refreshed per second of backend time, weighted by how
overdue the refresh is. Greedy admission by that ratio is the standard
fractional-knapsack rule, and it is the right objective because the corpus
metric this feeds — how many postings carry a fresh `last_seen` — is exactly
the numerator.

Concretely, on the measured platform means: Greenhouse is 65 postings for 0.30 s
and Personio is 12.3 postings for 2.56 s, so at equal staleness Greenhouse
scores **45×** higher. That is correct and it is also why the second lane has to
exist.

The clamps are `internal/shard/cost.go`'s, unchanged: 1 ms because a source that
finished instantly still costs a connection, 30 minutes because the 07/26
semaphore-leak run truncated ~216 Workday tenants at two minutes each and one
run like that must not dictate every future plan.

### 4.2 Back-off

```
eligible(s)  ⟺  ConsecutiveFailures == 0
             ∨  now − LastAttempt ≥ target(s) × 2^min(ConsecutiveFailures, 6)
```

Measured from **`LastAttempt`, not `LastSuccess`**. A permanently failing source
has no `LastSuccess`, so an age measured from it grows without bound and the
gate stops holding — the prototype attempted a dead source 31 times in 90 runs
before this was fixed, and 6 times after (§8). The cap of 6 means a permanently
dead source is still retried about every 64 days: back-off must not silently
become retirement, because a board that returns 503 for a month and then comes
back has to be noticed.

No jitter. Jitter is the usual way to spread retries and it is forbidden here by
the determinism constraint; the spread comes from sources having different
`LastAttempt` values, which they do.

### 4.3 Cold start is optimistic in value and honest in cost

An unknown source gets:

- **stale = 4000** — the cap, the same as a source four targets overdue, so it
  ranks with the most neglected work rather than behind it;
- **yield = the platform's 75th percentile** — optimistic, so a new board is
  tried rather than starved;
- **cost = the platform's median** — *not* optimistic.

Optimism in cost is not conservatism, it is a lie to the budget: it admits work
the run cannot finish and converts it into truncated sources, which cost their
full duration and refresh nothing. Optimism belongs in the value term, where
being wrong costs one source's worth of budget.

Measured cold-start behaviour, 8,145 sources at a 5-minute budget with an empty
state file: the first run admits 56,661 source-seconds against a 17,280-second
capacity — a 3.3× over-admission, because with no state at all even the platform
fallbacks are empty and everything is charged the 1,000 ms default. 5,893 of
8,145 sources completed, the dispatch gate declined 1,840, and by run 1 the
estimates had converged and over-admission was zero. **The first run after state
loss is a calibration run**; that is acceptable because it is also exactly what
every run does today.

### 4.4 The two lanes

```go
type Lane uint8
const (
    LaneAging Lane = iota // stale(s) ≥ Policy.ObligeAt (default 3000, i.e. 3× target)
    LaneValue
)
```

- **Aging lane** — ordered by `LastAttempt` ascending, then by `SourceID`. Strict
  FIFO. Reserved a share of each group's capacity (`Policy.ObligeShare`, default
  50%).
- **Value lane** — ordered by `score` descending, then by `SourceID`.

Admission is one pass: the aging lane against its reserved share, then aging
candidates that did not fit against the remainder, then the value lane. Each
admission checks both the group budget and the global budget from §2.

`ObligeShare` doubles as the **cold-start reserve cap**. Never-attempted sources
sort first in the aging lane (`LastAttempt` is zero), so a registry that doubles
overnight — which is what happened in this tree between the measurement and this
document — floods the aging lane. Capping that lane at half of each group's
capacity is what stops a bulk import from freezing every existing source's
refresh for days.

### 4.5 The fairness property, stated precisely

> **Bounded-delay progress.** Let *s* be enabled (not retired, not in back-off)
> in affinity group *G*, and let *B<sub>G</sub>* = budget × parallelism(G) × fill
> × ObligeShare be *G*'s per-run aging capacity. Once `stale(s)` crosses
> `ObligeAt`, *s* joins a queue ordered by `LastAttempt`. A source's
> `LastAttempt` advances only when it actually runs, so no already-queued source
> can move ahead of *s*, and each run drains a prefix of the queue costing at
> least *B<sub>G</sub>* (or the whole queue). Therefore *s* is attempted within
> ⌈(W<sub>s</sub> + N) / B<sub>G</sub>⌉ runs, where W<sub>s</sub> is the
> predicted cost of the queue ahead of it on entry and N is the cost of sources
> newly registered since.

Two honesties about that statement. It is a bound on **delay, not on
staleness**: if a group's total demand exceeds its per-run capacity, some source
*must* miss its freshness target and the guarantee degrades to a rotation
period. And it depends on never-attempted sources being finite; a registry that
grows without bound starves everything, which is a registry problem the
scheduler correctly refuses to hide.

Measured, on the 07/28-shaped registry — the 12 platforms the measurement
covered, at their measured source counts, 3,268 of the 3,685 then registered —
at a 3-minute budget and 24 workers over 30 runs, a 4× cut from the budget that
measurement actually used:

| platform | sources | never crawled | p50 staleness | max |
| --- | ---: | ---: | ---: | ---: |
| greenhouse | 646 | 0 | 1 d | 1 d |
| smartrecruiters | 54 | 0 | 1 d | 1 d |
| workday | 210 | 0 | 1 d | 2 d |
| ashby | 417 | 0 | 2 d | 2 d |
| recruitee | 492 | 0 | 3 d | 3 d |
| **personio** | **970** | **0** | **3 d** | **5 d** |

Cheap high-yield platforms refresh daily; the 243-round platform rotates on
three to five days. Nothing is starved. At a 2-minute budget over the full
8,145-source registry the worst platform is SuccessFactors — 717 sources through
2-slot pods — at a 22-day maximum, still a rotation and still nothing at zero.

### 4.6 What the fairness lane costs, in the metric it hurts

The aging lane is not free, and the honest number is:

| policy, 3-min budget, 07/28 shape, 30 runs | postings refreshed/run | Personio sources never crawled |
| --- | ---: | ---: |
| value lane only (pure density) | **459,167** | **726 of 970** |
| two lanes (this design) | 395,815 | 0 |
| pure staleness FIFO, no scoring | 395,814 | 0 |

Ranking purely by density refreshes **16% more postings per run** and abandons
75% of Personio permanently — after 30 runs the 244 Personio sources it did
touch were 30 days stale. On the full 8,145-source registry the same policy
leaves 952 Breezy, 801 Personio and 413 Teamtailor sources never crawled in 60
runs. That 16% is the price of the fairness property, paid knowingly.

At budgets where the whole registry fits (6 minutes and up on the 07/28 shape)
all three policies are identical, which is the expected and reassuring result:
the scoring function only matters when it has to choose.

Pure FIFO matches the two-lane policy on both metrics *at this budget* because
at 3 minutes almost everything is more than three targets stale, so the aging
lane does nearly all the work. The value lane earns its place in the regime
between "everything fits" and "nothing is fresh", which is where a
well-configured nightly should sit.

### 4.7 Types

```go
type Budget struct {
    Wall      time.Duration // 0 with Unbounded=false means "no wall budget"
    Requests  int           // see below
    Unbounded bool          // daemon: refresh forever against a freshness target
}

type Policy struct {
    Target       time.Duration            // default 24h
    PerPlatform  map[string]time.Duration // sorted before use; never ranged over
    Tick         time.Duration            // clock quantisation, default 1h
    ObligeAt     int32                    // default 3000 (3× target)
    ObligeShare  int32                    // percent, default 50
    Fill         int32                    // percent, default 90
    ColdParallel int32                    // milli-units, default httpx.DefaultPerHostLimit×1000
    RetireAfter  time.Duration            // default 90 days
}

type Options struct {
    Now     time.Time
    Budget  Budget
    Policy  Policy
    Workers int // the crawl's --concurrency
    Shards  int // 1 unless the plan will be sharded
}

type Item struct {
    Source      SourceID
    Group       string
    PredictedMS int64
    Score       int64
    Lane        Lane
    StaleMilli  int32
    Rank        int
}

type Deferral struct {
    Source SourceID
    Reason string // "backoff" | "retired" | "group_budget" | "global_budget"
}

type Plan struct {
    SchemaVersion int
    PlanID        string
    PlannedFor    time.Time // the quantised Now, so the plan says what it assumed
    Budget        Budget
    Workers       int
    Shards        int
    Items         []Item     // execution order
    Deferred      []Deferral // sorted; why every unplanned source was unplanned
    Groups        []GroupBudget
}

func Build(sources []services.Source, store *Store, opts Options) (Plan, error)
```

`Requests` is specified and **not implementable today**: `services.SourceRun`
carries no request count, which `docs/architecture-roadmap.md` already lists as
missing ("per-service request, retry and 429 aggregates are still not in the
manifest"). When that field lands, a request budget is the same greedy pass with
`cost` in requests and no parallelism divisor, because requests do not become
cheaper by being concurrent. Until then `Build` rejects a `Requests` budget
rather than approximating one.

`Unbounded` (daemon) sets both capacity terms to infinity and admits everything
with `stale ≥ 1000`, ordered identically. The daemon re-plans every `Tick`. Same
code, no second scheduler.

---

## 5. Execution: stopping cleanly

### 5.1 Emit order, and why it is not the selection order

Selection ranks by value. **Execution order is a different problem** — it
decides how well the worker pool is used — and getting it wrong is expensive in
a way that is invisible in the plan.

`internal.AllWithConcurrency` hands a source to a worker, and the worker then
blocks inside `httpx` on that service's semaphore. A worker parked on a
semaphore is *occupied*, not idle. So putting 970 Personio sources at the head
of the list parks 60 of 64 workers on one 4-slot key.

The rule: **longest-processing-time first within a group, groups interleaved
round-robin by descending group load.** LPT within a group minimises that
group's makespan; interleaving spreads the opening wave across backends, which
is the same reason `shard.Plan.Resolve` deliberately keeps the registry's
platform round-robin instead of re-sorting.

Measured with a discrete-event model of the pool
(`scratchpad/proto/tools/execsim`; 3,268 sources at 07/28 shape, 24 workers, real
affinity keys and real limiter slot counts):

| emit order | wall clock, everything admitted | sources done in 12 min |
| --- | ---: | ---: |
| registry order (today) | 1,427 s | 1,828 |
| grouped, one platform at a time | 1,453 s | 1,851 |
| cost descending, groups ignored | 1,110 s | 858 |
| **LPT within group, groups interleaved** | **1,015 s** | **2,654** |

29% less wall clock for identical work, and 45% more sources finished under a
12-minute cap. The model puts useful utilisation at 48% for the winning order,
which is the number the real 07/28 run measured — encouraging, and quite
possibly a coincidence, since the model's per-source costs are synthetic.

One honest failure mode: LPT is the right heuristic when the plan fits the
budget and the wrong one when it does not. Given a work set three times too
large for the budget, LPT completes 451 sources where registry order completes
1,345, because it spends the whole budget on the expensive head. The protection
is admission sizing (§2, `fill`) plus §5.2, and the residual risk is real —
see §10.

**The aging lane wins dispatch order as well as admission**: within a group,
aging-lane items sort ahead of value-lane items, before the LPT comparison. Without
that, a truncated run never reaches the tail of any group's LPT order, and the
tail is by construction the cheapest sources. The prototype starved the cheapest
half of every group for 100 consecutive runs before this rule existed (§8).

### 5.2 The dispatch gate

A run stops by **declining to start work it cannot finish**, not by being killed
mid-source:

```go
// Gate reports whether a source should still be started. It is consulted at
// dispatch, inside the worker, so it sees real elapsed time rather than the
// plan's prediction.
type Gate func(ctx context.Context, src services.Source) bool

func (p Plan) Gate(now func() time.Time, deadline time.Time) Gate
```

The default gate admits *s* when `now() + predicted(s) ≤ deadline`. A declined
source is recorded with a new terminal lifecycle status, **`deferred`**, and:

- `deferred` changes **nothing at all** in the state — not even `LastAttempt`.
  Advancing `LastAttempt` on a deferral resets the aging clock of a source that
  never ran, which is unbounded starvation dressed as fairness.
- `deferred` must be added to `shard/manifest.go`'s `terminalSourceStatuses`,
  and must **not** be added to `shard/cost.go`'s `costSampleStatuses`: its
  duration is zero because it did not run, and feeding that to the cost
  estimator would make deferred sources look free and re-admit them forever.
- `deferred` is not a failure and must not count toward
  `failedSourceRatio`'s ceiling in `shard/merge.go` — and, less obviously, must
  not sit in its **denominator** either. That function divides failures by
  `len(allSources)`; leaving deferrals in the denominator dilutes the ratio and
  quietly raises the failure count a run can hide. The denominator becomes
  sources actually attempted.

The gate needs no change to `internal/all.go`. It is a decorator over
`internal.JobsFunc`, the same shape `services.Observe` already uses:

```go
// In internal/services, one new option rather than a new function:
func Observe(sources []Source, logger *slog.Logger, opts ...ObserveOption) ([]internal.JobsFunc, func() []SourceRun)
func WithGate(g func(context.Context, Source) bool) ObserveOption
```

Sources still get the run's `context.WithTimeout` as a backstop, so a source
whose prediction was badly wrong is still cut off, still recorded `truncated`,
and still — per §5.3 — not blamed for it.

### 5.3 Folding a manifest back into state

```go
func Fold(store *Store, m shard.Manifest, now time.Time) *Store
func FoldAll(store *Store, ms []shard.Manifest, now time.Time) *Store
```

Per `services.SourceRun.Status`:

| status | LastAttempt | LastSuccess | DurationMS | Postings | Failures |
| --- | --- | --- | --- | --- | --- |
| `complete` | set | set | push | push | reset to 0 |
| `failed` | set | — | push | — | +1 |
| `truncated` | set | — | push | — | **unchanged** |
| `stopped` | set | — | push | — | **unchanged** |
| `deferred` | — | — | — | — | — |
| `planned` / `running` | — | — | — | — | — |

Three rules worth their own sentences:

- **Our impatience is not the board's fault.** `truncated` and `stopped` mean we
  ran out of budget or a consumer broke early. Counting either as a failure
  would put a slow-but-healthy source into exponential back-off because of a
  scheduling decision we made — which is the tail of the 07/26 run turning into
  a permanent blindspot.
- **Their durations still count**, exactly as `shard/cost.go` already decides:
  they are lower bounds, and treating a lower bound as no information is how the
  most expensive sources end up looking cheap.
- **A failed source's posting count is never pushed.** A zero from an outage
  would drag the yield median down and demote the source for a week.

Group parallelism is folded from the same manifest by the estimator in §2.
Sources present in the state but absent from the registry are marked `Retired`;
retired records older than `RetireAfter` are dropped. Registry sources absent
from the state are simply missing, which is the cold-start path.

`FoldAll` is what the sharded merge calls: N shard manifests, one fold, one
writer.

---

## 6. Determinism

Same state plus same budget yields the same plan, byte for byte.

**Time is data.** `Options.Now` is a field, not a clock call. `Build` truncates
it to `Policy.Tick` (default one hour), so a retried workflow run inside the
same hour produces the identical plan — and the plan records `PlannedFor` so it
says what it assumed. A daemon sets `Tick` to a minute.

**Arithmetic is integer.** Not because Go's float64 is non-deterministic on one
machine — it is — but because the Go spec permits an implementation to fuse
`x*y+z` into a single rounding, and it does. Measured, same source,
`go build -gcflags=-S` (`scratchpad/fma`):

| GOARCH | instructions for `(y*s + b) / c` |
| --- | --- |
| amd64 | `MULSD`; `ADDSD` — two roundings |
| arm64 | `FMADDD` — one rounding |
| ppc64le | `FMADD` — one rounding |

The portability CI job builds linux/amd64, linux/arm64, darwin/arm64 and
windows/amd64, so a float score is a plan that can differ by architecture. How
much: on a grid of the measured per-platform yields and costs, 0.77% of scores
differ in the last ULP between fused and unfused evaluation (22% on a broader
random input range). I looked for an ordering flip in 5,000,000 random pairs and
**found none** — a flip needs two candidates within one ULP, which I could
construct by hand but did not observe naturally. So the honest statement is: the
risk is small, non-zero, entirely avoidable, and "we did not observe it" is not
what a stated invariant should rest on. Integers also make a golden plan file
exact rather than approximate.

**Nothing else leaks.** Inputs are sorted before iteration; every tie breaks on
`SourceID`; no map is ranged over into an artifact; `Build` starts no goroutines
and does no I/O. `PlanID` is `sha256` over `(rank, platform, key)` truncated to
16 bytes, the same construction `shard.planID` uses.

**Measured cross-target**: the golden fixture plan (5,000 sources, 4-minute
budget) produces `plan_id=1ce3a7ae4f59da2529f49ee6f0b791e3` under both
`linux/amd64` natively and `GOOS=js GOARCH=wasm` under node 22. No arm64
emulator was available in this container, so arm64 equality is argued from the
absence of floating point, not measured.

**Build cost**: 26.5 ms and 12.5 MB allocated for 8,145 sources (`go test
-bench`, 4-vCPU container). Against a 720-second crawl that is 0.004%. It is
also more than it needs to be — the platform-fallback percentiles are recomputed
per call — and worth trimming only if the daemon re-plans every second.

---

## 7. Composing with `internal/shard`

**One rule: schedule first, shard second, over one shared notion of affinity.**

```go
func (p Plan) Refs() []shard.SourceRef            // the selected sources
func (p Plan) Costs() map[shard.SourceRef]int64   // this plan's predictions
```

```
schedule.Build(registry, state, opts) ──▶ Plan
                                           │
                    shard.Build(plan.Sources(registry), shard.Options{
                        ShardCount: n,
                        Costs:      plan.Costs(),
                        SourceSetID: shard.SourceSetID(registry),  // always the whole registry
                    })
```

Why this order and not the other:

- The plan must be **one artifact** for the merge's coverage proof. Scheduling
  inside each shard would give N independent selections, no single plan ID, and
  nothing for `shard merge` to verify "exactly once" against.
- The **global capacity term** (§2) and the **cold-start reserve** are
  properties of the run, not of a shard. Applying them per shard applies them N
  times.
- The **per-group term needs no adjustment**, because an affinity group lives in
  exactly one shard by construction. This is why the composition is clean at
  all.

`Shards` enters `Build` only through the global capacity term
`budget × workers × shards × fill`. That is the budget model's "sharding buys
latency, not budget" expressed as arithmetic: more shards raise the global term
and leave every per-group term untouched, so they buy throughput for
tenant-isolated platforms and buy a shared backend nothing at all.

**No second affinity table.** `schedule` calls `shard.AffinityKeys`, which asks
`httpx.ServicePolicyForHost`. One table (`httpx`), one derivation (`shard`), two
consumers.

**`shard plan --prior` should read the state file.** It currently re-parses a
directory of manifests to compute the same median-of-samples estimate the state
file now maintains — `docs/design/corpus-format.md` §3.5 asks for exactly this
change. Add `--state`; keep `--prior` working; where both are given, state wins
for the sources it covers.

**`shard merge --state-out`** folds every shard manifest into the next state, in
the merge job, single-writer.

A measured side effect worth naming: budget-aware selection **makes shard
packing easier**. Because the scheduler caps each group at
`budget × parallelism`, no indivisible bin can exceed the budget, which removes
the largest-group floor that `docs/architecture-roadmap.md` identified as the
reason shard counts above 8 buy nothing. Measured over the full registry with a
10-minute budget (`scratchpad/proto/tools/composesim`): 7,612 sources selected,
zero groups split across shards, and shard estimates balanced to within 0.005%
at both 4 and 8 shards. Wall clock is still the budget, by construction — what
extra shards now buy is more sources inside it.

**What none of this fixes.** Personio is 970 tenants through one 4-slot key:
970 × 2.56 s / 4 = 621 s of group time, and no budget, plan or shard count
lowers it, because splitting it across runners is the pressure increase the
politeness invariant forbids. Breezy is now worse at 373 rounds. The scheduler's
entire contribution is to stop that floor from being paid *every* run for 1.5%
of the postings — it can be paid every third run instead. Making it actually
cheaper is adapter work: fewer requests per tenant, or a bulk endpoint if one
exists.

---

## 8. Testing

Untestable scheduling is how starvation bugs survive, so the seams come first.

**The seams.**

- `Options.Now` is a value. There is no clock interface, no `timeNow` variable,
  and no need for one.
- `Build` and `Fold` are pure: no I/O, no goroutines, no clock, no map iteration
  into output. A test is a fixture in, a struct out.
- `Fold` consumes a `shard.Manifest`, which is a JSON file, so the whole
  feedback loop — plan, run, fold, plan again — is drivable from `testdata`
  with no network.
- The dispatch gate takes `now func() time.Time`, so a fake clock is a closure
  over an `int64`.
- A **cost oracle** lives in the test harness, never in the scheduler:
  `type Oracle func(SourceID) (time.Duration, int, error)`. The simulator turns
  a plan into a manifest by consulting it, which is what makes multi-run
  properties assertable in milliseconds.

**The tests**, all of which the prototype runs
(`scratchpad/proto/internal/schedule/schedule_test.go`, whole suite 1.6 s):

| test | asserts |
| --- | --- |
| `TestDeterministicUnderInputPermutation` | 20 shuffles of the source slice give the same `PlanID` and the same order |
| `TestDeterministicUnderClockJitterWithinTick` | ±59 min of clock drift inside one tick does not change the plan |
| `TestGoldenPlanID` | a fixed fixture's `PlanID` matches a checked-in constant — the cross-target and cross-release guard |
| `TestMonotoneInBudget` | a larger budget admits a superset; no source is dropped by being given more time |
| `TestGroupBudgetRespected` | per group, Σ predicted ≤ budget × parallelism |
| `TestNoStarvation` | 200 runs at a budget that admits a few percent of the set per run: every source visited, worst staleness 17 days |
| `TestDeferredSourcesDoNotAge` | 100 runs where the executor completes only half the plan: every source still runs |
| `TestBackoffBoundsAttempts` | a permanently failing source is attempted 5–12 times in 90 runs — neither hammered nor retired |

Plus: a **fuzz target on `Decode`** (state files are downloaded artifacts and
therefore untrusted input, the same standing `shard.Plan.Validate` gives a plan);
a **round-trip property test** `Decode(Encode(s)) == s` over a generated store; a
**golden state file** in `testdata` so a serialisation change is a visible diff;
and running the schedule package's golden test under `GOOS=wasip1` in the
portability CI job, which is one package and a few seconds.

**Two bugs these tests found in the prototype**, both of which changed the
design above:

1. `TestBackoffBoundsAttempts` failed at 31 attempts in 90 runs. Back-off was
   timed from `LastSuccess`; a permanently failing source has none, so its age
   grew without bound and eventually cleared every interval. Fixed by timing
   back-off from `LastAttempt` (§4.2).
2. `TestDeferredSourcesDoNotAge` failed with `greenhouse/k0000` never run in 100
   runs. LPT emit order puts the cheapest sources at the tail of every group, so
   a run truncated at the same fraction each time never reaches them. Fixed by
   giving the aging lane priority in dispatch order, not only in admission
   (§5.1).

Neither is exotic and neither would have been caught by inspection.

**What the multi-run simulations are and are not.** `schedsim` and `execsim`
run against the real registry, the real affinity keys and the real limiter slot
counts, with per-source durations drawn deterministically from a lognormal
around the measured per-platform means — and around a flat 1.5 s assumption for
the nine platforms the 07/28 run did not cover. They are evidence about
*ordering, fairness and admission*, and they are not a prediction of wall clock.
The first real budget-bounded run replaces every number in §4.5 and §4.6.

---

## 9. Delivery

Follows `docs/crawl-budget-model.md`'s migration, with one reordering argued
below.

1. **`internal/schedule`: `SourceState`, `Store`, `Decode`/`Encode`, `Fold`.**
   Plus `total --state` writing it and `schedule status` reading it. No
   scheduling change; no output change. Two runs later there is real cost data.
2. **`shard plan --state`**, replacing the manifest-directory scan with the
   state file. Pure simplification, same estimator.
3. **`Build`, plus `total --plan-only`** to print what a given budget *would*
   do. Compare against a full pass for a week; the plan's predicted duration
   against the manifest's actual is the calibration this whole design rests on.
4. **The dispatch gate and the `deferred` status**, with `total --budget`
   bounding the run.
5. **`shard merge --state-out`**, and the nightly cut over to a budget.

**Where I disagree with the migration order.** The budget model bounds the run
(its step 3) before accumulating a corpus (its step 4). Those cannot be in that
order. `jobs_record.txt` records one run's posting count, and `track_jobs.yml`
guards it with `MIN_POSTINGS_PCT=50` / `MIN_SOURCES_PCT=60` against the last
recorded value. A budget-bounded run legitimately visits a fraction of the
registry, so its total is a fraction of yesterday's, and the guard fires — or
worse, is relaxed and stops catching the outage it exists to catch. Either the
corpus lands first, so the recorded number is the corpus's open count rather
than the run's yield, or `total` keeps an explicit full-pass mode for the record
while the budget path proves itself. **The corpus is the prerequisite for
bounding the nightly, not a follow-up to it.**

---

## 10. What I rejected, and what I am unsure about

**Rejected.**

- **Rank by staleness alone (pure FIFO).** Simple, fair, and ignores a 45×
  spread in cost per posting. It ties the two-lane policy at a 3-minute budget
  because at that budget nearly everything is in the aging lane anyway, and it
  has nothing to offer in the regime where a well-configured nightly should sit.
- **Rank by value density alone.** Measured: 16% more postings refreshed per
  run, and 726 of 970 Personio sources never crawled in 30 runs (952 Breezy, 801
  Personio, 413 Teamtailor on the full registry over 60 runs). The failure is
  total and silent.
- **A flat sum of durations as the budget.** Measured: 38% of the budget used
  and 26% less work than critical-path accounting. It is the intuitive model and
  it is wrong.
- **Curating a per-platform concurrency table for the planner.** A second
  opinion about what `httpx` already knows, guaranteed to drift, and the drift
  looks like a rate-limit problem. Measuring parallelism from manifests gets
  pacing and cooldowns for free.
- **EWMA instead of a median of trailing samples.** One bad night moves an EWMA;
  `shard/cost.go` already chose a median for exactly this reason and there is no
  case for two estimators.
- **Random jitter on back-off.** The standard fix, forbidden by the determinism
  constraint. `LastAttempt` already spreads retries.
- **Persisting the priority queue between runs.** It is derivable from state.
  Persisting it adds a second source of truth and a migration for no gain.
- **A database for 1.5 MB of state.** `docs/design/storage-engine.md` settles
  this with measurements; nothing here needs transactions, indexes or a second
  writer.
- **Preemption — killing a running source to fit the budget.** It throws away
  the whole cost, produces a `truncated` source that cannot advance `last_seen`,
  and pressures a backend for nothing. The dispatch gate declines instead.
- **Scheduling inside each shard.** Breaks the single-plan coverage proof and
  applies the global budget N times.
- **Per-source freshness targets in the state file.** State would become
  configuration, and configuration in a file that a run rewrites is
  configuration that a run can silently change. Targets live in `Policy`.

**Unsure, and worth attacking next.**

- **LPT is wrong when the plan overruns.** Admission sizing is supposed to make
  that rare (measured: 0 sources cut per run in steady state), and it did not
  make it rare on the cold-start run. A `--reorder-on-overrun` that switches the
  remaining tail to shortest-first once elapsed exceeds a threshold would fix
  it, at the cost of an execution order that depends on wall clock — which is a
  real determinism cost, since the *plan* stays deterministic but the *outcome*
  no longer is. I did not resolve this and would not add it before a real
  bounded run shows it matters.
- **`ObligeAt = 3× target` and `ObligeShare = 50%` are guesses.** They are
  guesses with a measured consequence (§4.6) but nobody swept them. A sweep over
  the simulator is cheap and should precede the cutover.
- **Yield is a proxy for value.** Postings-refreshed weights a 40,000-posting
  Workday tenant far above a 12-posting Personio board, which is right for corpus
  freshness and arguably wrong for a job hunter. The number that would settle it
  — posting churn per source — needs two consecutive corpus generations, which
  `docs/design/corpus-format.md` also flags as its largest unmeasured
  assumption. `yield` is a single term and swapping it for churn is a one-line
  change.
- **No live budget-bounded run exists.** Every multi-run number here is
  simulated. The 07/28 measurement itself came from a 4-vCPU container at
  concurrency 24, not a GitHub runner, and its own caveat applies to everything
  built on top of it.
- **Nine of 22 platforms have no measured cost at all**, including the two
  largest by source count. Their simulated behaviour rests on a flat 1.5 s
  assumption. The first run that folds a manifest into state replaces that with
  fact, which is a reason to land step 1 of §9 before anything else.
