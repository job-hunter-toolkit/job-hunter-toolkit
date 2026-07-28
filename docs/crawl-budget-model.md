# The crawl budget model

The crawler today has one execution model: **one process attempts every source
once, and the result is either complete or partial.** That model has now failed
on its own terms for weeks — the last full run recorded 473,404 postings from
1,772 sources, still unfinished after 350 minutes — and the registry has since
grown to 3,685 sources.

More importantly, it is the wrong model for where this project is going. A
GitHub Actions job, a laptop, a daemon, a phone, and a service worker in a
browser tab differ by four orders of magnitude in how much work they can do
before they are interrupted. A design whose only two outcomes are "everything"
and "not everything" cannot serve any of them well.

This document replaces it.

## The consequence that reorders everything: sharding does not save money

It is worth stating plainly because the opposite is intuitive. Eight runners for
45 minutes bills the same 360 minutes as one runner for 360 minutes, plus eight
times the checkout, toolchain and cache-restore overhead. **Sharding buys
latency, not budget.** It is the right tool for "the crawl must finish before
09:00", and the wrong tool for "CI costs too much".

The thing that reduces cost is doing less work per run. And the thing that makes
less work per run *acceptable* is that the work does not need to be done all at
once — which is the same property that lets the crawler run in a browser tab.

## The model: a refreshed corpus, not a complete pass

Stop asking "did the crawl finish?" and start asking "how stale is each source?"

- A **corpus** is the accumulated set of postings, each carrying `first_seen`
  and `last_seen`.
- A **run** is a bounded amount of work against that corpus: refresh as many
  sources as the budget allows, most valuable first.
- **Coverage is a rolling window,** not an event. Every source is visited within
  its freshness target; no single run has to visit all of them.

This is strictly more informative than what exists today. A corpus with
`first_seen`/`last_seen` per posting answers questions the current snapshot
cannot: when did this role appear, how long do postings at this company stay
open, what closed this week. For a job-hunting tool those are the questions that
matter most, and they fall out of the model rather than needing to be built.

### What a run needs to persist

A small per-source record — roughly 3,685 rows today, well under a megabyte:

```text
source id (platform + key)
last attempted, last succeeded
last duration, last posting count
consecutive failures
```

`total --manifest` already emits per-source lifecycle, duration, posting count
and error class, and `shard plan --prior` already accepts prior manifests to
weight packing by measured duration. **The inputs exist; nothing consumes them
across runs.** That is the gap.

### Scheduling

Given a budget, order sources by value per unit cost:

```text
value  ≈ expected postings × staleness against target
cost   ≈ measured duration from prior runs (unknown sources get an optimistic
         default so they are tried rather than starved)
```

Three properties this must have, each learned from a failure already in this
repository's history:

- **Bounded and interruptible.** A run stops cleanly at its budget and keeps
  what it got. Partial work is the normal case, not the error case.
- **Fair.** A permanently slow source must not monopolise every run, and a
  permanently failing one must back off rather than burn budget forever. The
  consecutive-failure count is what drives that.
- **Deterministic given the same state.** A retried run must not reshuffle,
  for the same reason `shard plan` is already deterministic.

## What each surface gets from the same primitive

| Surface | Budget | What it does |
| --- | --- | --- |
| GitHub Actions nightly | minutes, and it is the cost centre | Refresh the staleest slice; commit corpus delta + state |
| Sharded Actions run | wall-clock bound | Same plan, partitioned by affinity; merge as today |
| CLI one-shot | seconds | `--company` narrows the plan; unchanged behaviour |
| Daemon | continuous | Refresh forever, freshness target instead of a deadline |
| Browser / PWA | one tab, one user, CORS-limited | Crawl the handful of sources behind the query, cache in IndexedDB, fall back to the published corpus |
| Service worker | background, throttled | Same, on a timer, with a tiny budget |
| Phone | hostile to long work | Query the published corpus; refresh opportunistically |

One scheduler, one state record, one corpus format. The surfaces differ only in
the budget they pass and the storage they attach — which is exactly the
`storage.Backend` split `docs/surfaces-and-extensibility.md` already specifies.

## Cost control in GitHub Actions

Concrete, in the order they pay off:

1. **Bound the nightly.** A 60-minute freshness-driven run instead of a
   350-minute exhaustive attempt is a ~6x reduction, and produces *fresher* data
   because every run spends its whole budget on what is most stale rather than
   re-fetching what was fetched yesterday.
2. **Do not run the full matrix on documentation.** Four portable builds,
   staticcheck and govulncheck on a docs-only change is pure waste; `paths-ignore`
   fixes it. This branch's own research commits triggered all nine checks.
3. **Cache the toolchain and module downloads.** `actions/setup-go` caches by
   default from v4; confirm it is actually hitting rather than assumed.
4. **Keep `cancel-in-progress` on CI and off the crawl.** Already correct today,
   and worth not regressing: cancelling a five-hour crawl at minute 300 throws
   away the whole day.
5. **Publish the corpus as a release artifact, not a committed blob.** A daily
   committed dataset grows the clone for every user and every CI checkout
   forever.

The failure mode to design against is not a large bill; it is CI being disabled
and the project losing its only source of live verification. Everything above
degrades gracefully: a bounded run that gets less done is still correct, because
partial work is the normal case in this model.

## Migration

This does not require a rewrite, and must not become one.

1. **Persist what the manifest already reports.** A state file written at the
   end of a run and read at the start of the next. No scheduling change yet.
2. **Order by that state.** Staleness-first ordering, still attempting every
   source. Immediately useful, trivially revertible.
3. **Bound the run.** Stop at the budget instead of at the source list. This is
   the step that cuts cost, and it is safe only once ordering is trustworthy.
4. **Accumulate a corpus** with `first_seen`/`last_seen`, superseding the
   single-number `jobs_record.txt` row while continuing to emit it.

Steps 1 and 2 are pure additions and change no output. Step 3 changes what a
nightly run means, and needs the same care the sharded cutover documents: run it
beside the existing path, compare, and only then move the schedule.

## What this does not change

- A partial crawl is still never recorded or graphed **as complete**. The status
  field and the partial-vs-complete distinction stay exactly as they are; this
  model makes partial the expected case, which makes labelling it correctly more
  important, not less.
- Totals are still a global union, never a sum of shards.
- The politeness ceiling is still per-service and still lives in `httpx`.
  Budget-aware scheduling decides *what* to fetch, never *how hard*.
