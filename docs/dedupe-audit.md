# URL dedupe audit

`internal.Dedupe` keys on `JobPosting.URL`. This is an audit of what every
adapter in `internal/services` actually puts in that field, and of whether
`Dedupe` should normalise URLs before comparing them.

It was prompted by the Phenom/Lowe's defect recorded in
`deletedDoubleCountRoutes`: the Phenom adapter yielded each posting's `applyUrl`,
which for that tenant was the *Workday* posting URL with `/apply` appended, so
5,103 postings were counted twice on a suffix. The question this answers is
whether that was a Lowe's quirk or a class.

**It was a class, and it was not confined to Phenom.**

## Summary

| | |
| --- | --- |
| Adapters yielding another vendor's apply URL | **2** — Phenom (fixed here), Jibe (measured, recommended, not changed) |
| Phenom postings that carried another vendor's URL | **17,158 of 17,714 — 96.9%** |
| Jibe postings that carry another vendor's URL | **164,143 of 414,311 — 39.6%** |
| Postings published with a URL that cannot be opened | **4,250** (fixed here) |
| Postings with an empty URL | **0** |
| Boards collapsing to a single URL (the Gem defect) | **0** |
| Registry pairs measured as double counted through this defect | **3** — 1,933 postings; one already deleted |
| Recommended change to `internal.Dedupe` | **None.** See "Should Dedupe normalise?" |

## How this was measured

One full crawl with `--no-dedupe --concurrency 8`, emitting `source_platform,
source_key, company, url, title, location, requisition_id, external_id` as CSV:

```
1292062 postings from 8153 sources (10 sources failed)
```

7,969 sources returned at least one posting; 1,278,491 distinct URLs. Analysed
offline. Board-to-board pair comparisons were separate targeted crawls of both
sides. Every number below came from one of those two, none is extrapolated.

That mattered, because the code was misleading in exactly the places that count.
The Phenom field is named `applyUrl` and was read as if it were a posting URL.
The Jibe field is named `apply_url` and is populated with **818 different hosts**
depending on the tenant, including plain relative paths.

**Caveat on dates.** This crawl ran with the Phenom adapter as it was *before*
the fix below, which is deliberate — it is the only way to measure what the
defect cost. `phenom/careers.kbr.com` was still registered when it ran and has
since been deleted, so the corpus contains one pair the registry no longer has.
Every figure that depends on that is called out.

## 1. What each adapter puts in `URL`

Every adapter, the field it reads, and the shape it produces. "Canonical" means
the URL is a posting page on the board this project actually crawled.

| Platform | URL comes from | Shape | Verdict |
| --- | --- | --- | --- |
| ashby | `jobUrl` | `jobs.ashbyhq.com/<co>/<uuid>` | canonical (Ashby also publishes `applyUrl`; correctly unused) |
| bamboohr | list URL + `?id=` | `<co>.bamboohr.com/careers/list?id=<id>` | canonical — BambooHR's own link form |
| brassring | `Link` | `sjobs.brassring.com/…?partnerid&siteid&jobid` | canonical, gateway-scoped |
| breezy | `position.url` | `<co>.breezy.hr/p/<id>-<slug>` | canonical |
| direct | hand-written per employer | employer domain | canonical |
| gem | built from `extId` | `jobs.gem.com/<co>/<extId>` | canonical |
| greenhouse | `absolute_url` | `job-boards.greenhouse.io/…`, or the employer's own site with `?gh_jid=` | canonical |
| **jibe** | **`apply_url`** | **another vendor's apply URL — 818 distinct hosts** | **apply URL** |
| jobvite | anchor `href` | `jobs.jobvite.com/<co>/job/<id>` | canonical |
| lever | `hostedUrl` | `jobs.lever.co/<co>/<uuid>` | canonical (Lever's `applyUrl` is this + `/apply`; correctly unused) |
| oraclecloud | built | `<host>/hcmUI/CandidateExperience/en/sites/<site>/job/<id>` | canonical |
| peopleforce | resolved `/careers/v/` href | `<co>.peopleforce.io/careers/v/<id>-<slug>` | canonical |
| personio | built | `<co>.jobs.personio.de/job/<id>` | canonical |
| **phenom** | **`applyUrl`** | **another vendor's apply URL — 16 distinct hosts** | **apply URL — fixed here** |
| pinpoint | `posting.url` | `<co>.pinpointhq.com/en/postings/<uuid>` | canonical |
| radancy | resolved `href` | employer domain `/job/<city>/<slug>/<n>/<id>` | canonical |
| recruitee | built | `<co>.recruitee.com/o/<slug>` | canonical |
| rippling | `job.url` | `ats.rippling.com/<co>/jobs/<uuid>` | canonical |
| smartrecruiters | built | `jobs.smartrecruiters.com/<co>/<id>` | canonical |
| successfactors | built, `career_ns=job_application` | `career<N>.successfactors.{com,eu}/career?company&career_job_req_id&career_ns` | apply-shaped; see below |
| teamtailor | `item.url` | `<co>.teamtailor.com/jobs/<id>-<slug>` | canonical |
| workable | `job.url` | `apply.workable.com/j/<shortcode>` | canonical — `apply.workable.com` *is* Workable's board host |
| workday | tenant URL + `externalPath` | `<tenant>.wd<N>.myworkdayjobs.com/<site>/job/…` | canonical |

**Query parameters that vary per fetch: none.** The one candidate was
SuccessFactors' `_s.crb` token, which appeared in the apply URLs Phenom's
zimmerbiomet tenant published. Fetched twice three seconds apart it was
byte-identical, so it is tenant configuration rather than a per-request nonce. It
is gone from this project's output anyway now that Phenom yields its own URLs.

**Tracking parameters: 1,781 postings**, all of them Jibe's Mount Sinai tenant,
whose `apply_url` is an Oracle Cloud URL carrying
`utm_source=external+career+site&utm_medium=career+site`. They collide with
nothing, because that Oracle tenant is not registered.

### SuccessFactors is apply-shaped but is not the same defect

`successFactorsApplyURL` builds `…/career?company=X&career_job_req_id=Y&career_ns=job_application`.
That is an application route, not a listing route. It is left alone deliberately:

- It is **deterministic and platform-unique**. Every one of the 98,000-odd
  SuccessFactors URLs in the corpus has exactly those three parameters in that
  order on one of five hosts. Two SuccessFactors routes to one requisition would
  produce an identical string and `Dedupe` would collapse them.
- Swapping `career_ns` to `job_listing` would rewrite every SuccessFactors URL
  and buy nothing for dedupe. Both spellings answer 200 to a logged-out client
  (checked live), so there is no link-quality argument either.

It *looked* dangerous only because Phenom's zimmerbiomet and bechtel tenants
published SuccessFactors apply URLs for the same tenants — with a different path
(`/careers` against `/career`), extra parameters and a different order. That was
Phenom's defect, not SuccessFactors'.

## 2. Phenom: the Lowe's defect was 12 of 14 tenants

Reading the first page of every tenant in `PhenomCompanies` and bucketing
`applyUrl` by host. Across the full crawl, **17,158 of 17,714 Phenom postings —
96.9% — carried a URL on a host that is not the tenant's own.**

| Phenom tenant | `applyUrl` points at | Underlying tenant |
| --- | --- | --- |
| careers.conagrabrands.com | Workday | `conagrabrands.wd1…/Careers_US` |
| careers.dupont.com | Workday | `dupont.wd5…/Jobs` |
| careers.humana.com | Workday | `humana.wd5…/{Humana,CenterWell}_External_Career_Site` |
| careers.itw.com | Workday | `itw.wd5…/External` |
| careers.kbr.com | Workday | `kbr.wd5…/KBR_Careers` — **registered; route since deleted** |
| careers.oreillyauto.com | Workday | `oreillyauto.wd1…/oreilly` |
| careers.pentair.com | Workday | `pentair.wd5…/Pentair_Careers` |
| careers.ppg.com | Workday | `ppg.wd5…/PPG_CAREERS` |
| **careers.southwestair.com** | Workday | **`swa.wd1…/external` — registered** |
| **careers.zimmerbiomet.com** | SuccessFactors | **`career8…/zimmerin01` — registered** |
| jobs.bechtel.com | SuccessFactors | `career4…/Bechtel` |
| careers.united.com | Taleo | `ual-pro.taleo.net` |
| careers.mccain.com | *(no applyUrl)* | — |
| careers.molsoncoors.com | *(no applyUrl)* | — |

So on 12 of 14 tenants this adapter emitted a *different vendor's* URL on
postings whose `Source.Platform` says `phenom`. Three of those tenants are or
were registered on that other platform, and each double count was measured by
crawling both boards end to end:

| Pair | Postings | Distinct URLs | Raw URL overlap | Overlap after normalising |
| --- | ---: | ---: | ---: | ---: |
| phenom/careers.kbr.com vs workday/kbr | 1,566 / 1,565 | 1,558 / 1,565 | **0** | **1,556** after stripping `/apply` |
| phenom/careers.southwestair.com vs workday/swa | 18 / 43 | 18 / 43 | **0** | **15** after stripping `/apply` |
| phenom/careers.zimmerbiomet.com vs successfactors/zimmerin01 | 376 / 373 | 365 / 373 | **0** | **0** — no normalisation reaches it; 362 of 365 match on `career_job_req_id` |

**1,933 postings counted twice**, none of them visible to `Dedupe`. The KBR route
has since been deleted; Southwest and Zimmer are still both registered.

### Why the existing double-count guard did not catch them

`TestNoUnreviewedDoubleCountedEmployer` derives its subjects from company *names*
in `Builtin`. Two of the three pairs use a different name on each side —
`southwestair` against `swa`, `zimmerbiomet` against `zimmerin01` — so no overlap
was ever reported for them at all.

The third, KBR, did share a name, and `reviewedDoubleCounts` carried a row
saying `differentEmployers, "oraclecloud 2, personio 7, no shared title"`. No
Oracle Cloud or Personio tenant is named `kbr`; those two counts came from a
substring match against `brunswic**kbr**okers` and `lin**kbr**oker`. The test
asserts only that *a* row exists for the name, so a row about two unrelated
tenants silently pre-approved a 1,556-posting duplicate between the two platforms
that actually held the name.

**That is a hole in the guard, not just a stale row.** A row should record the
platform pair it measured and the test should require that pair to be the pair
actually registered. `internal/services/double_count_test.go` is outside this
change's scope, and its `kbr` row has since been corrected there.

### What was changed

`internal/services/phenom.go` now yields the tenant's own job-detail route,
`https://<tenant host>/us/en/job/<jobId>`, keeping `applyUrl` only as a fallback
for a tenant that ever stops publishing `jobId`.

Verified before changing it: `jobId` was non-empty and distinct on **every row of
a 100-row page from all 14 tenants**, and the canonical URL answered 200 with the
posting's own title rendered on the six tenants fetched end to end (kbr,
conagrabrands, zimmerbiomet, bechtel, united, mccain). The fallback means no
posting is dropped even if that stops being true.

**This does not by itself remove the double counts.** It makes the URLs honest —
a posting sourced from Phenom now links to Phenom — and it stops this project
republishing another vendor's application links under a `phenom` source label.
Collapsing Southwest and Zimmer still needs a route deleted, because one opening
genuinely has two different public URLs. The rows that deletion wants are at the
end of this document.

## 3. Jibe: the same defect, 40% of the platform's output

Jibe's `apply_url` is the only link the search payload carries — `slug` is the
requisition number, not a path — so this adapter has no correct field to switch
to. But `apply_url` is not one vendor's URL either. Across **414,311 Jibe
postings it resolved to 818 distinct hosts**, and 164,143 of them (39.6%) were
not iCIMS:

| Host | Postings | Tenant |
| --- | ---: | --- |
| `*.icims.com` (490-odd hosts) | 250,168 | most tenants; Jibe is iCIMS' modern career-site product |
| `fedex.wd1.myworkdayjobs.com` | 79,193 | fedex |
| `sjobs.brassring.com` | 28,178 | fedex |
| `fedex.paradox.ai` | 19,809 | fedex |
| `cta.cadienttalent.com` | 10,821 | petsmart |
| `genco.taleo.net` | 6,756 | fedex |
| `hrjcpyprd-dmz.jcpenney.com` | 5,928 | jcpenney |
| **(relative — no scheme or host)** | **4,250** | **fedex (4,249), jobs.aon.com (1)** |
| `mercy.wd1.myworkdayjobs.com` | 2,266 | mercy |
| `ejis.fa.us6.oraclecloud.com` | 1,781 | mountsinai |
| `www.workstream.us` | 800 | jobs.smoothieking.com |
| `rentokil-initial.workable.com` | 761 | rentokil |
| `recruiting.adp.com` | 749 | pepsico |
| `freight.wd108.myworkdayjobs.com` | 748 | fedexfreight |
| others (skillaz, axa.fr, driverapponline, stjude Workday, …) | ~2,000 | various |

Two distinct problems fall out of this.

### 3a. 4,250 postings had a URL that cannot be opened — fixed

Every relative URL in the whole 1.29-million-posting corpus came from Jibe, in
the form `/freight-apply/apply/POSTING-3-958978`. Stored verbatim, that is not a
URL. It passed the adapter's `link == ""` guard and it is unique, so `Dedupe`
kept every one: this project published 4,250 postings whose link goes nowhere,
and nothing anywhere reported a problem. That is exactly the silent-failure shape
this codebase keeps finding — not empty, not duplicated, just wrong.

`jibeApplyURL` now resolves a root-relative `apply_url` against the board's own
host. Verified live: that path answers **200 at `fedex.jibeapply.com`** and
**404 at `careers.fedex.com`**, so the board host is the right base and the
employer's vanity domain is not. Absolute and protocol-relative URLs are left
alone, and an empty one stays empty so the caller still drops the posting.

### 3b. FedEx: the prefix-and-suffix pair, and why it is not costing much

`reviewedDoubleCounts` carries `"fedex": {unmeasured, "jibe and workday; the
comparison timed out and has not been repeated"}`. It can now be partly settled.

79,193 of jibe/fedex's postings are Workday URLs on `fedex.wd1.myworkdayjobs.com`
across 12 Workday sites, and two of those sites are registered as
`workday/fedex`. The two adapters spell the same Workday posting differently:

```
jibe    https://fedex.wd1.myworkdayjobs.com/en-us/FXE-EU_External/job/FXE-EU…/Data-Keying-Agent…_RC720040/apply
workday https://fedex.wd1.myworkdayjobs.com/FXE-EU_External/job/FXE-EU…/Deputy-Station-Manager_RC772413-1
```

They differ by an `/en-us` **prefix** and an `/apply` **suffix** — the only
instance of that shape in the corpus besides Phenom's.

**But the boards are near-disjoint in content.** Comparing the registered site:
Workday's `FXE-EU_External` returns 337 postings (its own API reports
`total: 337`), Jibe's view of that same site carries 4,933, and after removing the
prefix and the suffix **exactly 1 URL matches**; on requisition id, also 1 of 338.

The reason is that Jibe's index is stale rather than that the boards differ.
Asking Workday's own CXS API for a requisition Jibe still advertises
(`RC720040`) returns **403 permission denied**, while a requisition Workday
itself lists returns 200 with the posting. So Jibe is serving apply links for
requisitions the Workday board has withdrawn.

Recorded honestly: **the FedEx double count is 1 posting today**, the URL
relationship is real, and the stale-index question is a data-quality problem for
a separate investigation, not a dedupe one. The `unmeasured` verdict should
become a measured one saying this.

### What was not changed, and what is recommended

Jibe's URLs were left alone. Unlike Phenom there is no existing field to prefer;
a canonical URL has to be synthesised, and getting it wrong would break a third
of the corpus.

It is worth doing, and the route is verified: **`https://<jibe board host>/jobs/<slug>`**
answered 200 and rendered the posting's own title on all four tenants tested
(costco, kehe, hazelden, appliedmedical — two `jibeapply.com` slugs and two
employer vanity hosts). `jibeHost` already computes the host and `slug` is already
decoded, so it costs no request. Before doing it, sweep all 247 tenants for a
non-200 or a soft-404, because it is 414,311 postings.

Two things to weigh first:

- Today, Jibe's foreign URLs are the *only* reason the FedEx relationship is
  visible at all. Switching Jibe to canonical URLs would hide it. Settle the
  FedEx row first, then change the URL.
- iCIMS apply URLs are host-independent, so two Jibe routes to one employer (a
  `jibeapply.com` slug and the employer's vanity host) currently collapse in
  `Dedupe` for free. Canonical board URLs would stop collapsing.

## 4. Radancy: the same architecture, no URL relationship at all

Radancy is a career-site front end like Phenom, but its adapter reads the site's
own `href`, so it yields canonical Radancy URLs — no adapter defect. It is here
because the *registry* consequence is identical: nine of fourteen Radancy tenants
are also registered on the platform behind them.

| Radancy tenant | Also registered as |
| --- | --- |
| att | workday/att |
| carnival | oraclecloud/carnival |
| chipotle | workday/chipotle |
| citi | workday/citi |
| disney | workday/disney |
| sanofi | workday/sanofi |
| veolia | successfactors/veolia |
| walgreens | brassring/walgreens |
| wegmans | workday/wegmans |

These share **no URL structure whatsoever** —
`www.att.jobs/job/chico/retail-sales-consultant/117/98395039840` against
`att.wd1.myworkdayjobs.com/ATTGeneral/job/…`. No normalisation can reach them and
none should try. They are registry decisions, and `reviewedDoubleCounts` is the
right place for them; the Chipotle row added there is the model.

## 5. Empty and identical URLs — the Gem defect is not recurring

| Check | Result |
| --- | --- |
| Postings with an empty URL | **0** |
| Postings with an unopenable (relative) URL | 4,250 — all Jibe, fixed above |
| Boards where every posting shares one URL | **0** |
| Sources with any repeated URL | 106 of 7,969 |
| Postings `Dedupe` drops as intra-source duplicates | 13,489 |
| …exact repeats (same URL, same title, same location) | 8,700 |
| …that differ in title or location | 4,789 |

The 8,700 exact repeats are real duplicates — pagination overlap, and boards that
list one requisition several times — led by Oracle Cloud (4,761), Phenom (2,244)
and BrassRing (1,530). `Dedupe` is right to drop them.

The 4,789 that differ are worth stating plainly, because they are `Dedupe`
deleting rows a job seeker might want:

- **4,691 from Workable.** Workable's widget API publishes one entry *per
  location* for a single `shortcode`, all carrying the same URL. `kreyco` returns
  4,878 entries for 914 requisitions; one shortcode covers six Illinois cities.
  The adapter is faithful to the API. Whether one requisition advertised in six
  cities is one posting or six is a product question, but note that `--location`
  filtering only ever sees whichever location arrived first.
- **98 from Rippling**, the same shape.

Nothing here argues for a URL change. It is recorded so the next person who sees
"13,489 postings dropped" does not read it as a URL defect.

Seven adapters build a URL with no guard against an empty one — ashby,
greenhouse, rippling, workday, oraclecloud, radancy and bamboohr. Two of them,
**workday** (`rawURL + externalPath`) and **radancy** (`base.ResolveReference`),
would collapse an entire board to one URL if the path component were ever empty,
which is precisely the Gem defect. Neither does today on any of 8,153 sources.
That is a latent risk worth a guard in those adapters, not an argument for
changing `Dedupe`.

### One place where cross-source dedupe does fire

Exactly **82 URLs in the corpus appear under more than one source**, and all 82
are Recruitee: four pairs of slugs (`ballysintralotsa`/`intralot`,
`onemobility`/`voltaira`, `gain`/`gainpro`,
`sportakademiebaumann`/`sportschuledefcon`) are aliases for one board, and
Recruitee canonicalises the URL to one subdomain in its payload. `Dedupe`
collapses them for nothing. It is the only case in 1.29 million postings where
the mechanism does the cross-source job it was written for, and it works because
the platform hands out one URL — not because anything normalised.

## Should `Dedupe` normalise URLs?

**No.** Measured, not argued.

Each candidate rule was applied to the 1,278,491 distinct URLs of the crawl,
counting how many URLs it merges and — the part that decides it — whether each
merge joins two *different* sources (the double count a rule is for) or two rows
of *one* source (where it destroys a real posting).

| Rule | URLs merged | Across sources | Within one source |
| --- | ---: | ---: | ---: |
| lowercase scheme and host | 0 | 0 | 0 |
| drop fragment | 0 | 0 | 0 |
| strip trailing slash | 0 | 0 | 0 |
| sort query parameters | 0 | 0 | 0 |
| **strip `/apply` suffix** | **1,505** | **1,505** | **0** |
| drop "tracking" parameters | 10,396 | 0 | **10,396** |
| drop the query string entirely | 246,612 | 189,765 | **56,847** |

Four of the seven merge nothing at all. **No two URLs in 1.29 million differ only
by case, by a fragment, by a trailing slash, or by parameter order.** A rule that
changes nothing is not worth changing what every consumer sees.

Two are destructive, and the measurement is unambiguous about it:

- **Dropping "tracking" parameters merges 10,396 URLs, every single one within a
  single board, and every merge joins postings with different titles.** The
  parameter is `gh_jid`: Greenhouse's `absolute_url` is often the employer's own
  careers page, where the job id lives entirely in the query.
  `mongodb.com/careers/job/?gh_jid=6275509` and `…?gh_jid=6381035` are two
  different Account Development Representative roles in two cities; the paths are
  byte-identical. Any rule that treats a query parameter as noise deletes one of
  them, and there is no safe allow-list, because `gh_jid` *is* the identifier on
  one platform and would be noise on another.
- **Dropping the query string entirely merges 246,612 URLs**, 56,847 of them
  inside one board. It would flatten every BrassRing gateway, every
  SuccessFactors tenant and every Cadient board to one URL per path.

### The one rule that works, and why it still should not exist

Stripping `/apply` is the interesting case and deserves a fair hearing, because
on this corpus it is **perfectly precise**: 1,505 merges, all 1,505 across two
sources, **zero within a source**. Every one of them is
`phenom/careers.kbr.com` against `workday/kbr` — the pair this audit found. On a
metric of "does it merge anything it shouldn't", it scores flawlessly.

It should still not go into `Dedupe`:

1. **It is measuring a bug that is now fixed.** Those 1,505 merges exist only
   because the crawl ran with the pre-fix Phenom adapter and with a route since
   deleted. Re-run against the current tree, the rule merges zero. A
   normalisation rule would have *hidden* this defect rather than surfacing it,
   and the defect was worth surfacing: it was also republishing another vendor's
   application links under a `phenom` source label, and it was the reason 4,250
   Jibe postings with unopenable URLs went unnoticed in the same field.
2. **It does not generalise.** It reaches KBR and Southwest, does nothing for
   Zimmer (different path *and* different parameters), nothing for FedEx without
   also stripping an `/en-us` prefix, and nothing for any of the nine Radancy
   pairs. A rule that fixes two of fourteen known pairs is not a solution to the
   class — it is a special case for one adapter's bug, living in the one file
   that is supposed to have no per-platform knowledge.
3. **`/apply` is a legitimate path segment.** `Dedupe` cannot know that
   `…/job/X/apply` and `…/job/X` are one posting on Workday but might be two
   pages on a board added next month. Zero within-source merges today is evidence
   about today's 22 platforms, not a guarantee.

### What to do instead

The right identity for a cross-platform duplicate is not the URL and cannot be
made into one. Two routes to one opening have genuinely different public URLs;
that is what a career-site front end *is*. The evidence points at two things:

1. **Resolve overlaps in the registry, with measurements** — which is exactly
   what `reviewedDoubleCounts` and `deletedDoubleCountRoutes` exist for. The gap
   is not the mechanism. It is that the guard keys on the company *name*, so it
   never saw `southwestair`/`swa` or `zimmerbiomet`/`zimmerin01`, and that a row
   is satisfied by any recorded verdict rather than one about the platform pair
   actually registered.
2. **If a code-level identity is ever wanted, it is the requisition id, not the
   URL.** It is the only field that survives a front end: Phenom's zimmerbiomet
   and SuccessFactors' zimmerin01 share 362 of 365 `career_job_req_id` values
   while sharing zero URLs. But it is empty on most platforms, it is not unique
   across employers, and scoping it by employer would change what "the same
   posting" means for every consumer. That is a design proposal, not a change to
   make inside `Dedupe`.

## Rows a maintainer should add if the two remaining Phenom routes are deleted

Recorded here so the measurements are not lost. These belong in
`deletedDoubleCountRoutes` in `internal/services/double_count_test.go`, which
this change does not modify.

```
"phenom/careers.southwestair.com": measured 2026-07-28: phenom 18 postings,
  workday swa.wd1.myworkdayjobs.com/external 43. Zero URLs matched as written;
  15 of 18 phenom URLs were exactly a workday URL with "/apply" appended,
  because the phenom adapter yielded applyUrl and that site is a front end onto
  the Workday tenant this project already crawls. Workday returns the whole
  board and phenom a subset. Keep workday. The name difference (southwestair
  against swa) is why TestNoUnreviewedDoubleCountedEmployer never reported it.

"phenom/careers.zimmerbiomet.com": measured 2026-07-28: phenom 376 postings over
  365 distinct URLs, successfactors zimmerin01 373. Zero shared URLs -- phenom
  published career8.successfactors.com/careers?company=zimmerin01&…&_s.crb=… and
  the successfactors adapter publishes /career?company=zimmerin01&… with
  different parameters in a different order -- but 362 of 365 career_job_req_id
  values are shared. No URL normalisation reaches this pair. Keep
  successfactors: it is the system of record and returns 8 more requisitions.
```

`fedex` should also move out of `unmeasured`, to a row recording that jibe and
workday overlap by 1 posting because Jibe's index carries requisitions Workday
has withdrawn — see section 3b.
