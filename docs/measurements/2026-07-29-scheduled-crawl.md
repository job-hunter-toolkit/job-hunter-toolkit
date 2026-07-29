# Scheduled crawl measurement, 2026-07-29

First live use of `internal/schedule`. Until today the package was complete,
tested and imported by nothing; `total --schedule` is the wiring, and this file
is the evidence that the wiring works. Two bounded runs, four minutes of budget
each, back to back against live boards. Every number here came out of those two
runs or the files they wrote — nothing is extrapolated and nothing is projected.

## What was added

`total` grows five flags, all off by default:

| flag | default | what it does |
| --- | --- | --- |
| `--schedule` | off | build a plan, gate dispatch against it, fold the result back into state |
| `--schedule-state` | `os.UserCacheDir()/job-hunter-toolkit/scheduler-state.jsonl` | where state lives |
| `--schedule-budget` | `--timeout` | wall budget the plan packs against |
| `--schedule-plan` | unset | dump the plan as JSON |
| `--schedule-dry-run` | off | build and report the plan, crawl nothing, touch no state |

Passing any of the last four without `--schedule` is an error rather than a
silent no-op: a workflow that passed `--schedule-budget` alone would look
bounded, crawl everything, and say nothing about it.

Without `--schedule` the command reads no file, builds no plan and consults no
gate. The single `DATE POSTINGS COMPANIES STATUS` row the nightly parses is
produced by the same code as before.

### Why not `/tmp`

`docs/posting-cache.md` argues at length that a predictable path in a
world-writable directory is a phishing primitive. State is less sensitive than
the posting URLs that argument was written about — source keys, durations and
counts, not what anyone searched for — but the file decides which boards the
next run talks to, so a path any local user can pre-create is a path any local
user can use to steer that. Default is `os.UserCacheDir()`, directory created
`0700`. `schedule.WriteFile` already publishes through `os.CreateTemp`, which
creates `0600`, and rename preserves it; the measured files below are `0600`.

## Run 1: cold state

```
$ jht total --schedule --schedule-budget=4m --concurrency=8 --timeout=6m \
      --schedule-state=state.jsonl --manifest=manifest1.json
schedule: state state.jsonl (cold, 0 sources, 0 groups), budget 4m0s, workers 8
schedule: plan 9d5d401d2d1a66850f34e48f07a77f09 planned 1728 of 8229 sources, 1728s predicted against 1728s capacity
schedule: lanes aging=1728 value=0
schedule: deferred global_budget=6501
07/29/26 253735 908 partial
exit 1
```

Wall clock 01:19:14Z to 01:25:15Z, 360 s. `source_counts`: **959 complete, 1
truncated, 7,269 deferred.** Exit 1 because the crawl hit its 6-minute context
deadline without `--allow-partial`, which is the pre-existing failing-closed
rule and is unchanged.

Everything about this plan is the documented cold start. No state means every
source is maximally stale (`stale_milli` 4000) and no cost is known, so all
8,229 sources sit in the aging lane at the same optimistic 1,000 ms estimate.
The global term — 240 s x 8 workers x 90 % fill = 1,728 s — is what bound it,
and it bound it uniformly: 1,728 sources at 1 s each, exactly filling capacity,
with the other 6,501 deferred `global_budget`.

That prediction is wrong by 2.1x. 960 of the 1,728 planned sources were
attempted, and those 960 consumed **2,061 s** of source time against the 960 s
the plan charged them. The dispatch gate declined the remaining 768. **The gate,
not the plan, is what bounded this run**, which is what the package says it is
for: the plan is a prediction, the gate spends it against real elapsed time.

One source overran the backstop. `jibe/dollargeneral` was still fetching page
183 when the 6-minute context expired, was recorded `truncated` at 164,117 ms,
and the fold gave it `consecutive_failures: 0`. Our impatience is not the
board's fault.

State after run 1: 214,121 bytes, 960 source records (959 with `last_success`),
10 group records. Measured durations across those 960: min 62 ms, p50 481 ms,
p90 2,511 ms, max 164,117 ms — a spread of 2,600x that the cold model's flat
1,000 ms cannot represent at all.

## Run 2: same budget, warm state

```
$ jht total --schedule --schedule-budget=4m --concurrency=8 --timeout=6m \
      --schedule-state=state.jsonl --manifest=manifest2.json
schedule: state state.jsonl (state, 960 sources, 10 groups), budget 4m0s, workers 8
schedule: plan 20d8a368b1307ce9ad190eefa06c69cf planned 4066 of 8229 sources, 1727s predicted against 1728s capacity
schedule: lanes aging=4064 value=2
schedule: deferred global_budget=4163
07/29/26 199731 644 partial
exit 0
```

Wall clock 01:25:51Z to 01:30:15Z, 263 s. `source_counts`: **650 complete, 1
failed, 7,578 deferred.** Exit 0, status `partial`.

### The plan is different, and different in the ways it should be

| | run 1 (cold) | run 2 (warm) |
| --- | ---: | ---: |
| plan id | `9d5d401d…` | `20d8a368…` |
| sources planned | 1,728 | **4,066** |
| predicted against 1,728 s capacity | 1,728 s | 1,728 s |
| aging lane / value lane | 1,728 / 0 | 4,064 / **2** |
| deferred at plan time | 6,501 | 4,163 |
| declined at dispatch | 768 | 3,415 |

**2.35x more sources for the identical budget.** Nothing about the budget
changed; the cost model did. A median measured source costs 481 ms against the
cold assumption of 1,000 ms, so the same 1,728 seconds of capacity buys more
than twice the sources.

**Only 770 of run 2's 4,066 items were also in run 1's plan; 3,296 are new.**
Those 770 are almost exactly the 768 the gate declined in run 1. That is the
FIFO-rotation property working: a declined source never advances `LastAttempt`,
so it returns at the head of the aging lane instead of being starved by whatever
sorts ahead of it.

**Two sources moved to the value lane.** 959 sources succeeded in run 1 and are
therefore ~0 % stale; their score (`yield x staleness / cost`) collapses and they
lose to everything overdue. The run does not re-fetch what it fetched four
minutes ago. Two got in on leftover capacity.

The platform mix inverted completely:

| platform | run 1 planned | run 2 planned |
| --- | ---: | ---: |
| oraclecloud | 0 | 1,203 |
| personio | 0 | 898 |
| recruitee | 0 | 492 |
| greenhouse | 432 | 460 |
| breezy | 432 | 248 |
| ashby | 417 | 235 |
| jibe | 222 | 63 |
| lever | 0 | 155 |
| pinpoint | 0 | 118 |

Plan 1 reached 10 distinct platforms; plan 2 reached 14.

### Measured parallelism cut every measured group's capacity

`FoldGroups` derived real concurrency from run 1's start and finish stamps, and
the second plan used it. All ten measured groups came in below the assumed 4.0,
five of them at the 1.0 floor:

| group | measured | capacity run 1 | capacity run 2 | planned run 1 | planned run 2 |
| --- | ---: | ---: | ---: | ---: | ---: |
| `platform:eightfold` | 3.54 | 864 s | 765 s | 18 s / 18 src | 0 |
| `platform:jibe` | **1.74** | 864 s | 375 s | 222 s / 222 src | 75 s / 63 src |
| `platform:brassring` | 1.46 | 864 s | 315 s | 33 s / 33 src | 0 |
| `service:icims` | 1.09 | 864 s | 235 s | 63 s / 63 src | 0 |
| `platform:direct` | 1.09 | 864 s | 234 s | 5 s / 5 src | 0 |
| `platform:ashby` | **1.00** | 864 s | 216 s | 417 s / 417 src | 32 s / 235 src |
| `platform:bamboohr` | 1.00 | 864 s | 216 s | 55 s / 55 src | 0 |
| `platform:breezy` | **1.00** | 864 s | 216 s | 432 s / 432 src | 107 s / 248 src |
| `platform:gem` | 1.00 | 864 s | 216 s | 51 s / 51 src | 0 |
| `platform:greenhouse` | 1.00 | 864 s | 216 s | 432 s / 432 src | 39 s / 460 src |

Two things fell out of this at once. Capacity dropped four-fold on every
measured group, so the scheduler stopped booking parallelism that no backend
delivered — and because measured per-source costs also dropped, greenhouse fit
**more** sources (460) into **less** capacity (39 s of 216 s) than it had
before. Ashby did the same: 235 sources in 32 s. The six groups that got zero in
run 2 — eightfold, brassring, icims, direct, bamboohr, gem — are the six run 1
finished entirely (planned count equals completed count for each), so every one
of their sources is fresh and scores near zero.

Plan 2 spanned 1,002 affinity groups; 10 carried a measurement and **992 were
flagged `measured: false`**, still on the assumption. After run 2 the state file
holds 656 measured group records, because run 2 reached the oraclecloud tenants,
each of which is its own service group.

### The bounded stop, and the status invariant

Run 2 was **not** truncated. The gate stopped admitting work at the 4-minute
budget and the 23-second tail was sources already in flight finishing normally;
263 s total, inside the 6-minute backstop, exit 0.

And it still reported `partial`, because it deliberately skipped 7,578 sources.
This is the invariant the change was most at risk of breaking, so it is enforced
from `shard.Manifest.Complete()` — status complete *and* every listed source
terminal — rather than from a skip counter written for the occasion, so the
scheduled row and the merge cannot end up with two definitions of finished. A
declined source is recorded `deferred`, which is not a terminal status.

Two supporting decisions make that check meaningful:

- **The manifest lists all 8,229 registered sources, not just the planned
  ones.** Listing only what ran would leave `Complete()` calling a run that
  skipped 7,000 sources finished. Unplanned sources are dispatched last, cost
  one gate call each, and are recorded `deferred`.
- **The failing-closed exit code stays tied to truncation, not to partiality.**
  Under `--schedule` a partial row is the designed outcome, not a missed
  deadline, so exiting non-zero for it would make the normal case look broken.
  The status field, not the exit code, is what keeps it distinct.

The extra `Complete()` condition is applied only when `--schedule` is set. It
would be a no-op on an unscheduled crawl, which cannot leave a source
unattempted without the context expiring — but "would be a no-op" is not "is a
no-op", and the existing row contract had to stay byte-identical.

### Worker utilisation went from 72 % to 93 %

Run 1 had 8 workers for 360 s — 2,880 worker-seconds available — and spent
2,061 s of them, **72 %**. Run 2 had 8 x 263 s = 2,104 available and spent
1,952 s, **93 %**. The difference is the shape of the stop: run 1's workers
idled from minute 4 to minute 6 because the gate had stopped admitting while one
164-second source held the run open, whereas run 2 ended as soon as its tail
drained.

### The cost model is better where it has looked, and still blind elsewhere

Run 2's plan charged its 4,066 items a mean 425 ms, drawn from run 1's medians:
`newEstimator` falls back platform median, then global median, and run 1's global
median was 481 ms. The 651 sources run 2 actually attempted averaged **2,998
ms** — 7x the estimate. 628 of them were `oraclecloud` tenants, a platform run 1
never touched, so they were charged the global median of a sample that contained
none of them.

That is the estimator working as specified rather than a defect: an unmeasured
source gets an optimistic default so it is tried rather than starved, and it
costs exactly one run's over-commitment to fix, which the gate absorbs. It does
mean the plan's predicted totals should not be read as a forecast until a source
has been seen at least once.

### Folding is faithful about blame

- `jibe/dollargeneral`, truncated by our own deadline at 164 s: duration
  recorded, `consecutive_failures: 0`.
- `radancy/aldi`, a real error: duration recorded, `consecutive_failures: 1`,
  `error_class` stored, no posting sample pushed.

## Determinism

Three consecutive dry runs against the frozen post-run-1 state produced byte
identical summaries and the same plan id:

```
schedule: plan 20d8a368b1307ce9ad190eefa06c69cf planned 4066 of 8229 sources, 1727s predicted against 1728s capacity
schedule: plan 20d8a368b1307ce9ad190eefa06c69cf planned 4066 of 8229 sources, 1727s predicted against 1728s capacity
schedule: plan 20d8a368b1307ce9ad190eefa06c69cf planned 4066 of 8229 sources, 1727s predicted against 1728s capacity
```

`Options.Now` is truncated to `Policy.Tick` (1 hour), so a workflow retried
inside the same hour re-plans identically and does not reshuffle work already
under way.

## State file size

| after | bytes | source records | group records |
| --- | ---: | ---: | ---: |
| run 1 | 214,121 | 960 | 10 |
| run 2 | 495,813 | 1,611 | 656 |

`docs/crawl-budget-model.md` estimated "roughly 3,685 rows today, well under a
megabyte". Measured, a source record with seven duration and seven posting
samples is larger than that estimate assumed: 2,267 records occupy 496 KB, about
219 bytes each, so a fully warmed 8,229-source file lands nearer **1.8 MB**.
Still small, still a single file, but the estimate in that document is low by
roughly 2x and should be corrected there rather than quietly forgotten here.

## What this run does not show

- **No shard integration.** `Plan.Costs()` and `Plan.Refs()` exist to hand a
  plan to `shard.Build`, and nothing calls them. `shard run` and `shard merge`
  still know nothing about state, and `FoldAll` — written specifically so that N
  shards have one writer — is still only ever called with one manifest.
- **No nightly change.** The workflow was not touched. Moving the nightly onto
  `--schedule` is the step `docs/crawl-budget-model.md` says must run beside the
  existing path and be compared first, and this is one comparison, not that.
- **No corpus.** These runs still count postings per run. `first_seen` /
  `last_seen` accumulation is step 4 of the migration and is untouched.
- **No throughput claim.** The two runs crawled almost disjoint source sets, so
  their postings-per-second numbers are not comparable and no conclusion about
  scheduling efficiency should be drawn from them. What is demonstrated is that
  the second plan differs from the first *because state changed*, which is the
  thing that was unproven.
- **Two policy knobs remain unswept.** `ObligeAt` and `ObligeShare` are still
  the honest guesses `internal/schedule/policy.go` labels them as. Nothing here
  measured them.
