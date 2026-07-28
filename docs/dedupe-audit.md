# URL dedupe audit

`internal.Dedupe` keys on `JobPosting.URL`. This is an audit of what every
adapter in `internal/services` actually puts in that field, measured against live
boards on 2026-07-28, and of whether `Dedupe` should normalise URLs before
comparing them.

It was prompted by the Phenom/Lowe's defect recorded in
`deletedDoubleCountRoutes`: the Phenom adapter yielded each posting's `applyUrl`,
which for that tenant was the *Workday* posting URL with `/apply` appended, so
5,103 postings were counted twice on a suffix. The question this answers is
whether that was a Lowe's quirk or a class.

**It was a class, and it was not confined to Phenom.**

## Summary

| | |
| --- | --- |
| Adapters yielding another platform's apply URL | **2** — Phenom (fixed here), Jibe (recommended, not changed) |
| Postings measured as double counted by it, in the registry today | **1,933** confirmed by URL, plus FedEx (see below) |
| Postings published with a URL that cannot be opened | **4,249** — Jibe/FedEx relative paths (fixed here) |
| Postings with an empty URL | **0** |
| Boards collapsing to a single URL (the Gem defect) | **0** |
| URLs shared by two different sources across the whole corpus | **0** |
| Recommended change to `internal.Dedupe` | **None.** See "Should Dedupe normalise?" |

## How this was measured

A full crawl with `--no-dedupe`, emitting `source_platform, source_key, company,
url, title, location, requisition_id, external_id` as CSV, then analysed offline.
Targeted board-to-board comparisons used `tools/dcprobe`'s approach against the
specific pairs. Figures below say which run they came from; nothing here is
extrapolated from code reading, because the code was misleading in exactly the
places that mattered — the Phenom field is named `applyUrl` and was read as if it
were a posting URL, and the Jibe field is named `apply_url` and is populated with
eight different vendors' URLs depending on the tenant.

## 1. What each adapter puts in `URL`

Every adapter, the field it reads, and the shape it produces. "Canonical" means
the URL is a posting page on the board this project actually crawled.

| Platform | URL comes from | Shape | Verdict |
| --- | --- | --- | --- |
| ashby | `jobUrl` | `jobs.ashbyhq.com/<co>/<uuid>` | canonical (Ashby also publishes `applyUrl`; correctly unused) |
| bamboohr | built from list URL + `?id=` | `<co>.bamboohr.com/careers/list?id=<id>` | canonical — BambooHR's own link form |
| brassring | `Link` | `sjobs.brassring.com/…?partnerid&siteid&jobid` | canonical, gateway-scoped |
| breezy | `position.url` | `<co>.breezy.hr/p/<id>-<slug>` | canonical |
| direct | hand-written per employer | employer domain | canonical |
| gem | built from `extId` | `jobs.gem.com/<co>/<extId>` | canonical |
| greenhouse | `absolute_url` | `job-boards.greenhouse.io/…`, or the employer's own site with `?gh_jid=` | canonical; 27 distinct hosts |
| **jibe** | **`apply_url`** | **another vendor's apply URL — 330 distinct hosts** | **apply URL** |
| jobvite | anchor `href` | `jobs.jobvite.com/<co>/job/<id>` | canonical |
| lever | `hostedUrl` | `jobs.lever.co/<co>/<uuid>` | canonical (Lever's `applyUrl` is this + `/apply`; correctly unused) |
| oraclecloud | built | `<host>/hcmUI/CandidateExperience/en/sites/<site>/job/<id>` | canonical |
| peopleforce | built | `<co>.peopleforce.io/careers/v/<id>-<slug>` | canonical |
| personio | built | `<co>.jobs.personio.de/job/<id>` | canonical |
| **phenom** | **`applyUrl`** | **another vendor's apply URL — 16 distinct hosts** | **apply URL — fixed by this change** |
| pinpoint | `posting.url` | `<co>.pinpointhq.com/en/postings/<uuid>` | canonical |
| radancy | resolved `href` | employer domain `/job/<city>/<slug>/<n>/<id>` | canonical |
| recruitee | built | `<co>.recruitee.com/o/<slug>` | canonical |
| rippling | `job.url` | `ats.rippling.com/<co>/jobs/<uuid>` | canonical |
| smartrecruiters | built | `jobs.smartrecruiters.com/<co>/<id>` | canonical |
| successfactors | built, `career_ns=job_application` | `career<N>.successfactors.{com,eu}/career?company&career_job_req_id&career_ns` | apply-shaped, but see below |
| teamtailor | `item.url` | `<co>.teamtailor.com/jobs/<id>-<slug>` | canonical |
| workable | `job.url` | `apply.workable.com/j/<shortcode>` | canonical — `apply.workable.com` *is* Workable's board host |
| workday | tenant URL + `externalPath` | `<tenant>.wd<N>.myworkdayjobs.com/<site>/job/…` | canonical |

**Query parameters that vary per fetch: none found.** The one candidate was
SuccessFactors' `_s.crb` token, which appears in the apply URLs Phenom's
zimmerbiomet tenant publishes. Fetched twice three seconds apart it was
byte-identical, so it is tenant configuration rather than a per-request nonce. It
is gone from this project's output anyway now that Phenom yields its own URLs.

**Tracking parameters: 1,781 postings**, all of them Jibe's Mount Sinai tenant,
whose `apply_url` is an Oracle Cloud URL carrying
`utm_source=external+career+site&utm_medium=career+site`. They collide with
nothing today because that Oracle tenant is not registered.

### SuccessFactors is apply-shaped but is not the same defect

`successFactorsApplyURL` builds `…/career?company=X&career_job_req_id=Y&career_ns=job_application`.
That is an application route, not a listing route. It is left alone deliberately:

- It is **deterministic and platform-unique**. Every SuccessFactors posting in
  the corpus has exactly the three parameters in the same order on one of five
  hosts. Two SuccessFactors routes to one requisition would produce the identical
  string and `Dedupe` would collapse them.
- Swapping `career_ns` to `job_listing` would change 98,284 URLs and buy nothing
  for dedupe. Both spellings answer 200 to a logged-out client (checked live), so
  there is no link-quality argument either.

The reason it *looked* dangerous is that Phenom's zimmerbiomet and bechtel
tenants published SuccessFactors apply URLs for the same tenants — with a
different path (`/careers` vs `/career`), different parameters and a different
parameter order. That was Phenom's defect, not SuccessFactors'.

## 2. Phenom: the Lowe's defect was 12 of 14 tenants

Reading the first page of every tenant in `PhenomCompanies` and bucketing
`applyUrl` by host:

| Phenom tenant | `applyUrl` points at | Underlying tenant |
| --- | --- | --- |
| careers.conagrabrands.com | Workday | `conagrabrands.wd1…/Careers_US` |
| careers.dupont.com | Workday | `dupont.wd5…/Jobs` |
| careers.humana.com | Workday | `humana.wd5…/{Humana,CenterWell}_External_Career_Site` |
| careers.itw.com | Workday | `itw.wd5…/External` |
| **careers.kbr.com** | Workday | **`kbr.wd5…/KBR_Careers` — registered** |
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
postings whose `Source.Platform` says `phenom`. Three of those tenants are
registered on that other platform right now, and the double counts were measured
by crawling both sides:

| Pair | Postings | Distinct URLs | Raw URL overlap | Overlap after normalising |
| --- | ---: | ---: | ---: | ---: |
| phenom/careers.kbr.com vs workday/kbr | 1,566 / 1,565 | 1,558 / 1,565 | **0** | **1,556** after stripping `/apply` |
| phenom/careers.southwestair.com vs workday/swa | 18 / 43 | 18 / 43 | **0** | **15** after stripping `/apply` |
| phenom/careers.zimmerbiomet.com vs successfactors/zimmerin01 | 376 / 373 | 365 / 373 | **0** | **0** — no normalisation reaches it; 362 of 365 match on `career_job_req_id` |

**1,933 postings counted twice**, none of them visible to `Dedupe`.

### Why the existing double-count guard did not catch them

`TestNoUnreviewedDoubleCountedEmployer` derives its subjects from company *names*
in `Builtin`. Two of the three pairs use different names on each side —
`southwestair` vs `swa`, `zimmerbiomet` vs `zimmerin01` — so no overlap was ever
reported for them. The third, KBR, does share a name, and `reviewedDoubleCounts`
carries a row for it:

```go
"kbr": {differentEmployers, "oraclecloud 2, personio 7, no shared title"},
```

That note describes a comparison of *Oracle Cloud against Personio*. Neither
platform holds the name `kbr` any more; the two routes that do are Phenom and
Workday, and they were never compared. The test only asserts that *a* row exists
for the name, so a stale row for a different pair of platforms silently
pre-approved a 1,556-posting duplicate. **This is a hole in the guard, not just a
stale row** — the row should record which platform pair it measured, and the test
should require the recorded pair to match the pair actually registered. That is
`internal/services/double_count_test.go`, which is not in this change's scope.

### What was changed

`internal/services/phenom.go` now yields the tenant's own job-detail route,
`https://<tenant host>/us/en/job/<jobId>`, and keeps `applyUrl` only as a
fallback for a tenant that stops publishing `jobId`.

Verified before changing it: `jobId` was non-empty and distinct on **every row of
a 100-row page from all 14 tenants**, and the canonical URL answered 200 with the
posting's own title rendered on the six tenants fetched end to end
(kbr, conagrabrands, zimmerbiomet, bechtel, united, mccain). The fallback means
no posting is dropped even if that stops being true.

**This does not by itself remove the double counts.** It makes the URLs honest —
a posting sourced from Phenom now links to Phenom — and it stops this project
from republishing another vendor's application links. Collapsing KBR, Southwest
and Zimmer still requires deleting a route, because one opening genuinely has two
different public URLs. The measurements above are what such a deletion needs, and
the three rows it would want in `deletedDoubleCountRoutes` are given at the end.

## 3. Jibe: the same defect, 38% of the platform's output

Jibe's `apply_url` is the only link the search payload carries — `slug` is the
requisition number, not a path — so this adapter has no correct field to switch
to. But `apply_url` is not one vendor's URL either. Over 215,576 Jibe postings it
resolved to **330 distinct hosts**:

| Host family | Postings | Which tenants |
| --- | ---: | --- |
| `*.icims.com` | 133,255 | most tenants; Jibe is iCIMS' modern career-site product |
| `fedex.wd1.myworkdayjobs.com` | 42,659 | fedex |
| `cta.cadienttalent.com` | 10,821 | petsmart |
| `fedex.paradox.ai` | 7,740 | fedex |
| `hrjcpyprd-dmz.jcpenney.com` | 5,928 | jcpenney |
| **(relative — no scheme or host)** | **4,249** | **fedex** |
| `sjobs.brassring.com` | 3,619 | fedex |
| `mercy.wd1.myworkdayjobs.com` | 2,266 | mercy |
| `ejis.fa.us6.oraclecloud.com` | 1,781 | mountsinai |
| `genco.taleo.net` | 1,329 | fedex |
| `recruiting.adp.com` | 749 | pepsico |
| others (skillaz, axa.fr, driverapponline, stjude Workday, …) | ~1,000 | various |

Two distinct problems fall out of this.

### 3a. 4,249 postings had a URL that cannot be opened — fixed

Every relative URL in the whole corpus came from Jibe's FedEx tenant, in the form
`/freight-apply/apply/POSTING-3-958978`. Stored verbatim, that is not a URL. It
passed the adapter's `link == ""` guard and it is unique, so `Dedupe` kept it —
this project published 4,249 postings whose link goes nowhere, and nothing
anywhere reported a problem.

`jibeApplyURL` now resolves a root-relative `apply_url` against the board's own
host. Verified live: that path answers **200 at `fedex.jibeapply.com`** and
**404 at `careers.fedex.com`**, so the board host is the right base and the
employer's vanity domain is not.

### 3b. FedEx is crawled twice, and this is the pair that hides it

`reviewedDoubleCounts` carries `"fedex": {unmeasured, "jibe and workday; the
comparison timed out and has not been repeated"}`. The URL evidence settles the
direction even before the counts: 42,659 of jibe/fedex's postings are Workday
URLs on `fedex.wd1.myworkdayjobs.com`, across 12 Workday sites, and two of those
sites — `FXE-EU_External` and `FXF-MEX-External` — are registered as
`workday/fedex`.

The two adapters spell the same Workday posting differently:

```
jibe    https://fedex.wd1.myworkdayjobs.com/en-us/FXE-EU_External/job/FXE-EU…/Data-Keying-Agent…_RC720040/apply
workday https://fedex.wd1.myworkdayjobs.com/FXE-EU_External/job/…
```

They differ by an `/en-us` **prefix** and an `/apply` **suffix** — the exact
"differ only by a prefix or suffix" shape this audit was asked to look for, and
the only instance of it in the corpus besides Phenom.

### What was not changed, and what is recommended

Jibe's URLs were left alone. Unlike Phenom there is no existing field to prefer;
a canonical URL has to be synthesised, and doing it wrong would break a third of
the corpus.

It is worth doing, and the route is verified: **`https://<jibe board host>/jobs/<slug>`**
answered 200 and rendered the posting's own title on all four tenants tested
(costco, kehe, hazelden, appliedmedical — two `jibeapply.com` slugs and two
employer vanity hosts). `jibeHost` already computes the host and `slug` is already
decoded. Before doing it, sweep all 247 tenants for a non-200 or a soft-404,
because this is 414,318 postings.

Two things a maintainer should weigh first:

- Today, Jibe's foreign URLs are the *only* reason the FedEx duplication is
  detectable. Switching Jibe to canonical URLs would hide it. Measure FedEx and
  decide the route first, then change the URL.
- iCIMS apply URLs are host-stable, so two Jibe routes to one employer (a
  `jibeapply.com` slug and the employer's vanity host) currently collapse in
  `Dedupe` for free. Canonical board URLs would stop collapsing.

## 4. Radancy: the same architecture, no URL relationship at all

Radancy is a career-site front end like Phenom, but its adapter reads the site's
own `href`, so it yields canonical Radancy URLs — no defect. It is listed here
because the *registry* consequence is the same: nine of fourteen Radancy tenants
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

These share **no URL structure whatsoever** — `www.att.jobs/job/chico/retail-sales-consultant/117/98395039840`
against `att.wd1.myworkdayjobs.com/ATTGeneral/job/…`. No normalisation can reach
them, and none should try. They are registry decisions, and
`reviewedDoubleCounts` is the right place for them; the Chipotle row added there
is the model.

## 5. Empty and identical URLs — the Gem defect is not recurring

| Check | Result |
| --- | --- |
| Postings with an empty URL | **0** |
| Boards where every posting shares one URL | **0** |
| Sources with any repeated URL | 70 |
| Postings `Dedupe` drops as intra-source duplicates | 17,234 |
| …of which the duplicates differ in title or location | 4,777 |

The 12,457 exact repeats are real duplicates — pagination overlap and boards that
list one requisition several times — and `Dedupe` is right to drop them. The
4,777 that differ are worth stating plainly, because they are `Dedupe` deleting
postings a job seeker might want:

- **4,691 from Workable.** Workable's own widget API publishes one entry *per
  location* for a single `shortcode`, all with the same URL. `kreyco` returns
  4,878 entries for 914 requisitions; one shortcode covers six Illinois cities.
  The adapter is faithful to the API. Whether one requisition advertised in six
  cities is one posting or six is a product question, but note that `--location`
  filtering only ever sees whichever location arrived first.
- **86 from Rippling**, the same shape.

Nothing here argues for a URL change. It is recorded so that the next person who
sees "17,234 postings dropped" does not read it as a URL defect.

Seven adapters build a URL with no guard against an empty one — ashby,
greenhouse, rippling, workday, oraclecloud, radancy and bamboohr — and two of
them, **workday** and **radancy**, would collapse a whole board to one URL if the
path component were ever empty, which is precisely the Gem defect. Neither does
today on any of 8,145 sources. This is a latent risk worth a guard, not a live
bug, and it belongs in those adapters rather than in `Dedupe`.

## Should `Dedupe` normalise URLs?

**No.** Measured, not argued.

Every normalisation rule was applied to the distinct URLs of the crawl, counting
how many URLs each rule merges, and — the part that decides it — whether each
merge is *across* two sources (the double count a rule is for) or *within* one
source (where it destroys a real posting).

| Rule | URLs merged | Across sources | Within one source |
| --- | ---: | ---: | ---: |
| lowercase scheme and host | 0 | 0 | 0 |
| drop fragment | 0 | 0 | 0 |
| strip trailing slash | 0 | 0 | 0 |
| strip `/apply` suffix | 0 | 0 | 0 |
| sort query parameters | 0 | 0 | 0 |
| drop tracking parameters | 248 | 0 | **248** |
| drop the query string entirely | 89,771 | 70,281 | **19,490** |

The first five rules are the ones that sound obviously safe, and they are: they
merge nothing at all. **No two URLs in this corpus differ only by case, by a
fragment, by a trailing slash, by an `/apply` suffix or by parameter order.** A
rule that changes nothing is not worth the risk of changing what every consumer
sees.

The two that do something are both destructive:

- **Dropping "tracking" parameters merges 248 URLs, every one of them within a
  single board, and every merge joins postings with different titles.** The
  parameter is `gh_jid`: Greenhouse's `absolute_url` is often the employer's own
  careers page, where the job id lives entirely in the query.
  `agilityrobotics.com/about/job-post?gh_jid=5777967004` is "Accountant III" and
  `…?gh_jid=5786075004` is "Buyer". Any rule that treats a query parameter as
  noise deletes one of them. There is no safe allow-list either, because
  `gh_jid` *is* the identifier for one platform and would be noise on another.
- **Dropping the query string entirely merges 89,771 URLs**, 19,490 of them
  inside one board. It would flatten every BrassRing gateway to one URL, every
  SuccessFactors tenant to one, and every Cadient posting to one per
  `POSTING_ID`, deleting the per-location rows PetSmart publishes.

### The rule that would have worked, and why it still should not exist

Stripping `/apply` scores 0 above only because this crawl already carries the
fixed Phenom adapter's predecessor's problem *and* its Workday counterpart in the
same run for only part of the registry. Against the targeted pair measurements it
is not zero at all: it collapses **1,556 KBR** and **15 Southwest** postings, and
it would collapse FedEx once the `/en-us` prefix is also removed.

It should still not go into `Dedupe`, for three reasons:

1. It fixes a symptom of one adapter reading the wrong field, and that adapter
   has now been fixed. A normalisation rule would have hidden the defect rather
   than surfacing it, and the defect was worth surfacing — it was also
   republishing another vendor's application links under a `phenom` source label.
2. It does not generalise. It reaches KBR and Southwest, does nothing for
   Zimmer (different path *and* different parameters), and nothing for any of the
   nine Radancy pairs. A rule that fixes two of fourteen known pairs is not a
   solution to the class.
3. `/apply` is a real path segment on some boards. `Dedupe` cannot know that
   `…/job/X/apply` and `…/job/X` are one posting on Workday but might be two
   pages elsewhere, and it is the one place in the codebase with no per-platform
   knowledge and no way to acquire any.

### What to do instead

The right identity for a cross-platform duplicate is not the URL and cannot be
made into one. Two routes to one opening have genuinely different public URLs;
that is what a career-site front end *is*. The evidence in this audit points at
two things:

1. **Resolve overlaps in the registry, with measurements**, which is what
   `reviewedDoubleCounts` and `deletedDoubleCountRoutes` already exist for. The
   gap is not the mechanism, it is that the guard keys on the company *name* and
   so misses `southwestair`/`swa` and `zimmerbiomet`/`zimmerin01` entirely, and
   that a row is satisfied by any recorded verdict rather than one about the
   platform pair actually registered.
2. **If a code-level identity is ever wanted, it is `RequisitionID`, not the
   URL.** It is the only field that survives the front end: Phenom's zimmerbiomet
   and SuccessFactors' zimmerin01 share 362 of 365 `career_job_req_id` values
   while sharing zero URLs. But it is empty on most platforms and is not unique
   across employers, so it would need to be scoped by employer and would change
   what "the same posting" means for every consumer. That is a design change and
   belongs in a proposal, not in this audit.

## Rows a maintainer should add if the three Phenom routes are deleted

Recorded here so the measurements are not lost. These belong in
`deletedDoubleCountRoutes` in `internal/services/double_count_test.go`, which is
not modified by this change.

```
"phenom/careers.kbr.com": measured 2026-07-28: phenom 1,566 postings over 1,558
  distinct URLs, workday kbr.wd5.myworkdayjobs.com/KBR_Careers 1,565. Zero URLs
  matched as written; 1,556 of 1,558 phenom URLs were exactly a workday URL plus
  "/apply", because the phenom adapter yielded applyUrl and that site is a front
  end onto the same Workday tenant. 1,327 of 1,331 phenom titles are among
  workday's. Keep workday: it returns 7 more postings and is the system of record.

"phenom/careers.southwestair.com": measured 2026-07-28: phenom 18, workday
  swa.wd1.myworkdayjobs.com/external 43. Zero URLs matched as written; 15 of 18
  phenom URLs were a workday URL plus "/apply". Workday returns the whole board
  and phenom a subset. Keep workday.

"phenom/careers.zimmerbiomet.com": measured 2026-07-28: phenom 376 postings over
  365 distinct URLs, successfactors zimmerin01 373. Zero shared URLs — phenom
  published career8.successfactors.com/careers?... and the successfactors adapter
  publishes /career?... with different parameters — but 362 of 365
  career_job_req_id values are shared. Keep successfactors: it is the system of
  record and returns 8 more requisitions.
```

`careers.kbr.com` is also the case that shows `reviewedDoubleCounts` needs its
`kbr` row corrected: it records an Oracle Cloud versus Personio comparison for a
name now held by Phenom and Workday.
