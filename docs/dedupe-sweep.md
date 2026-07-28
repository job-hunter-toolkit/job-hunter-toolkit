# The scheduled dedupe sweep

`internal/services/double_count_test.go` guards against one employer being
crawled twice, and it keys on the company **name**. An employer registered under
a different name on each side is invisible to it.

That is not a hypothetical gap. Two real double counts sat behind it while the
test stayed green:

| Employer | Routes | Postings counted twice | What the guard saw |
| --- | --- | ---: | --- |
| Southwest Airlines | `phenom/careers.southwestair.com` vs `workday/swa.wd1` | 15 | nothing — `southwestair` ≠ `swa` |
| Zimmer Biomet | `phenom/careers.zimmerbiomet.com` vs `successfactors/zimmerin01` | 362 | nothing — `zimmerbiomet` ≠ `zimmerin01` |

Both were found by a person running a URL sweep by hand. `docs/dedupe-audit.md`
says closing the gap properly means comparing **boards** rather than names, which
is a crawl and not a unit test.

`tools/dedupesweep` is that crawl, and `.github/workflows/dedupe_sweep.yml` runs
it weekly so it happens on a schedule instead of when someone remembers.

## What it compares

Every source in the registry against every other, on three kinds of evidence and
never on titles.

| Evidence | Reaches | Threshold to report |
| --- | --- | --- |
| Shared raw URL | two routes publishing byte-identical URLs — already collapsed by `internal.Dedupe`, so this costs requests rather than count | 1 |
| Shared URL after normalising | the career-site front-end shape: `/apply` appended (Lowe's 4,729, KBR 1,556, Southwest 15) or an `/en-us` locale segment prepended (FedEx) | 1 |
| Shared requisition id | the only identity that survives a career-site front end, and the only thing that reached Zimmer Biomet | 5, **and 50% of *each* board** |

Any key held by more than four sources is discarded as a generic string rather
than an identity.

**Titles and locations are deliberately not evidence.** `double_count_test.go`
records what they cost: Visa was flagged as a duplicate on "2 of 2 shared
titles", and the two titles were the bare words "Sr. Manager" and "Director";
Chipotle shares 52 of 55 titles across two boards that share 12 of 178
title+location pairs, because "General Manager" recurs at thousands of
restaurants. A weekly report that reddened on a shared title would be switched
off within a month.

### Why the requisition threshold is 50% of *each* board

Every number in that last row was moved by a live run. Three false positives got
it there.

**1. Bare counters collide.** The first live run reported `eightfold/fluor`
against `jibe/carenewengland` — Fluor Corporation and Care New England,
unrelated employers — sharing **136 requisition ids, 24% of each board**. The
shared ids were `6535`, `7365`, `7414`: four-digit counters. Two dense
sequential numbering schemes are bound to collide.

**2. Scoring against the smaller board is meaningless.** With the share measured
against the smaller side, a partial sweep reported 22 pairs, and seven of them
were small boards against `jibe/dunhamssports`. Dunham's publishes 1,610
postings numbered densely enough to cover any small board's range, so a
9-posting board matches **100% of itself and 0.6% of Dunham's**. The share is now
required on both sides. Domino's against the University of Kansas — 55 shared
ids, 59% of the smaller board — is 0.2% of the larger, and is gone.

**3. A letter in the id is decoration, not entropy.** An earlier version of this
sweep counted ids carrying a letter separately from plain numbers and held plain
numbers to a higher bar, on the theory that `JR-02561381` is distinctive where
`6535` is not. Measured against the corpus, that distinction is worthless:

| Platform | Requisition id | What it is |
| --- | --- | --- |
| workday | `R242668` | a counter with a prefix |
| brassring | `38126BR` | a counter with a suffix |
| gem | `R267` | a counter with a prefix |
| successfactors | `451001` | a counter |

`brassring/guess` against `brassring/publix` shared 116 of those decorated
counters at 31% of each board — Guess and Publix, a fashion label and a
supermarket. The letter carried no information at all, so the split was deleted
and there is one rule for every requisition id.

**What separates a real pair is the share, not the shape.** Zimmer Biomet was
362 of 365 and 362 of 373. The real pair a partial sweep found — two BrassRing
gateways for UnityPoint under different names — was 147 of 147 and 147 of 149.
The highest false positive measured was 33%. The default sits at 50%: a real
margin over the collisions and far under both true pairs.

**This is a stated limitation, not a claim of completeness.** An employer whose
two routes overlap only partly, with no URL relationship at all, sits below this
threshold and this sweep will not report it. Requisition ids in this corpus do
not carry enough identity to find that case without also reporting Fluor against
Care New England every week, and a weekly report with a weekly false positive is
a report nobody reads.

## What it does with a finding

It prints it. Nothing else, and by design:

- The workflow runs with `contents: read`, like `verify_candidates.yml`. It
  cannot commit, and must not: acting on a finding means deleting a route from
  the registry, and that judgement has measured counter-examples on both sides.
  Home Depot serves 22,899 hourly and store roles through BrassRing and 972
  corporate roles through Workday with zero overlap; a rule that deleted a route
  per finding would have thrown away the half of that employer this project
  covers least well.
- **A finding never fails the build.** A false positive that reddened CI would
  get the scheduled job disabled, and then the blind spot would be open again
  under a green checkmark.

The report splits its findings in two, derived from the registry rather than from
a hardcoded list, so it cannot go stale:

- **Different company names** — the blind spot. Nothing in this repo can see
  these; a new one is a real finding.
- **Same company name** — `TestNoUnreviewedDoubleCountedEmployer` already
  requires a recorded verdict for these. They are printed for their numbers: a
  pair drifting toward total overlap is a route that has become a mirror and
  should be re-decided.

## Running it by hand

```
go run ./tools/dedupesweep -dump sweep.ndjson > report.md   # whole registry
go run ./tools/dedupesweep -platform phenom,workday -dump p.ndjson
go run ./tools/dedupesweep -in sweep.ndjson                 # re-analyse, no crawl
```

`-dump` writes one JSON row per posting in `tools/dedupeprobe`'s format, so the
two commands' output is interchangeable: a sweep dump can be re-analysed with
`-in` after a threshold change without touching a job board again, and a
two-board `dedupeprobe` run can be fed straight back in.

Quoting the evidence requires a dump. Without one the counts still stand, but
"shares 362 requisition ids" is not something a maintainer can check in one
request; with one, the report prints three of the ids.

## Cost

GitHub Actions minutes are this project's main running cost, so the schedule is
chosen against a measurement rather than a preference.

| | |
| --- | ---: |
| Sources swept | 8,173 |
| Postings compared | 1,296,529 |
| Wall clock, 4-vCPU container, one IP, concurrency 8 | **45m55s** |
| Budget ladder on a runner | 75 min sweep < 85 min step < 100 min job |
| Weekly cost | ~200 min/month measured, ~325 worst case |

The nightly crawl runs about 30 times a month, so this adds roughly six nightly
crawls' worth of minutes.

Daily would be seven times that bill to detect something that changes when a
source is added or an adapter's URL field changes — a pull-request event, not a
daily one. The two duplicates this exists to catch sat undetected for months; a
week is not the binding constraint on finding the next one, and a job nobody
wants to pay for gets deleted.

If the bill needs cutting further, cut scope before frequency: a dispatch with
`platform` set sweeps one ATS in a fraction of the time, and every duplicate
found so far has been a career-site front end (Phenom, Radancy, Jibe, Eightfold)
against the system behind it (Workday, SuccessFactors, BrassRing).


## What the first full run found

Run on 2026-07-28: **8,173 sources, 1,296,529 postings, 3,370,206 evidence keys,
45m55s**. 196 sources returned nothing and 9 reported an error; the report says
in as many words that those are unmeasured rather than clean.

Two caveats on how these numbers were produced. The crawl ran with the
thresholds this command shipped with *before* two of them were corrected by what
that same crawl found; the findings below come from re-analysing the one dump
with `-in` and the thresholds documented above, which is the whole point of the
dump existing. And the crawl used the adapters as they stood when it started —
`internal/services/jibe.go` was being changed in another branch of the same
working tree at the time, and a change to what Jibe puts in `URL` would move the
FedEx row.

**Seven pairs, six of them under different company names.**

| A | B | postings | shared URLs | after normalising | shared req ids | overlap |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| `recruitee/ballysintralotsa` | `recruitee/intralot` | 39 / 39 | 39 | 39 | 0 | 100% |
| `recruitee/onemobility` | `recruitee/voltaira` | 26 / 26 | 26 | 26 | 0 | 100% |
| `recruitee/gain` | `recruitee/gainpro` | 13 / 13 | 13 | 13 | 0 | 100% |
| `recruitee/sportakademiebaumann` | `recruitee/sportschuledefcon` | 8 / 8 | 8 | 8 | 0 | 100% |
| **`brassring/unitypoint,25790,5083`** | **`brassring/unitypointmeriter,25790,5084`** | 147 / 149 | **0** | **0** | **147** | **99%** |
| **`eightfold/houstonisd`** | **`successfactors/hisd,C0000167672P,…`** | 298 / 299 | **0** | **0** | **239** | **81%** |
| `jibe/fedex` | `workday/…/FXE-EU_External` (same name) | 138,185 / 346 | 0 | 7 | 7 | 0% |

The four Recruitee rows are the alias pairs `docs/dedupe-audit.md` already
measured (82 URLs then, 86 now). They share **raw** URLs, so `internal.Dedupe`
collapses them and nothing is counted twice; the cost is duplicate requests.

The FedEx row reproduces the audit's finding from the other direction, and is
the check that the URL normalisation works: Jibe emits
`/en-us/FXE-EU_External/job/…/apply` where Workday emits `/FXE-EU_External/job/…`,
and stripping the locale prefix and the `/apply` suffix matched 7 postings out
of 138,185 and 346. It is on the same-name list, where the unit test already
requires a verdict, and the verdict already recorded is the right one.

### Two new double counts, neither visible to any existing test

**UnityPoint — 147 postings, two BrassRing gateways.** `brassring.go` already
documents that `unitypoint` and `unitypointmeriter` share partnerid 25790.
Nobody had checked whether they serve the same requisitions. They do:

```
req 12026BR
  unitypoint,25790,5083        …?partnerid=25790&siteid=5083&PageType=JobDetails&jobid=750926
  unitypointmeriter,25790,5084 …?partnerid=25790&siteid=5084&PageType=JobDetails&jobid=750926
```

Same requisition, same BrassRing `jobid`, two URLs differing only in `siteid`.
`Dedupe` keys on the whole URL, so all 147 are counted twice. 147 of
`unitypoint`'s 147 and 147 of `unitypointmeriter`'s 149.

**Houston ISD — ~296 postings, Eightfold against SuccessFactors.** Registered as
`houstonisd` on one side and `hisd,C0000167672P,career4.successfactors.com` on
the other, which is why the name-keyed guard never reported it:

```
req 10972  "Helper, All Sports Hrly @ Barnett Multiple Vacancies"
  eightfold/houstonisd  https://apply.houstonisd.org/careers/job/171816194318
  successfactors/hisd   https://career4.successfactors.com/career?company=C0000167672P&career_job_req_id=10972&career_ns=job_application
```

Identical titles, identical requisition ids, no URL relationship of any kind —
the Zimmer Biomet shape exactly. A direct two-board comparison of the same crawl
finds **296 of 298 requisition ids shared**; the sweep reports 239 because the
"held by more than four sources" filter also drops ids this pair genuinely
shares. **A shared count in the report is a floor, not the overlap.** Settle a
pair with `tools/dedupeprobe`, which applies no thresholds at all.

Neither of these is fixed here. Both want a route deleted or a verdict recorded
in `internal/services/double_count_test.go`, which is a judgement and belongs to
whoever owns that file.

### Threshold sensitivity, measured on the same crawl

| `-min-req-share` | pairs | what changes |
| ---: | ---: | --- |
| 0.1 | 117 | counter collisions everywhere |
| 0.2 | 18 | |
| 0.3 | 8 | Guess vs Publix (31%) appears — a fashion label and a supermarket |
| **0.5** | **7** | the shipped default |
| 0.7 | 7 | identical |
| 0.9 | 6 | Houston ISD (81%) is lost |

| `-max-sources-per-key` | pairs | what changes |
| ---: | ---: | --- |
| 2 | 6 | Houston ISD lost; UnityPoint under-reads 101 instead of 147 |
| **4** | **7** | the shipped default |
| 8 | 7 | identical |
| 32 | 8 | Houston ISD reads 293, and one 7-id coincidence returns |

Both defaults sit on a plateau rather than on a cliff edge, which is the only
reason to trust them.

## Order matters more than concurrency

Worth recording, because this command got it wrong first and the cost was 15x.

Ranging over `services.Builtin` directly gives registration order, which is one
platform at a time. A bounded worker pool then spends its whole first wave inside
a single ATS: eight workers on Breezy's 1,492 tenants, all behind one limiter key
at four concurrent, left half the pool parked on a semaphore. Measured, that
walked **1,418 of 8,173 sources in 24 minutes**.

`services.SourcesMatching(nil)` returns the same sources interleaved across ATS
families, which is what the crawl uses and what `interleaveSources` in
`internal/services/builtin.go` was written for. The same eight workers then have
work on ~24 independent backends at all times. Politeness is unchanged either
way — `httpx`'s per-service limiter is what bounds pressure on any one backend,
not the pool width.
