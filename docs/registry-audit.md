# Registry audit, 2026-07-28

The registry tripled today, from 2,211 sources to 8,145. A probe proves a board
answered; it does not prove the adapter reads it correctly. This is a hunt for
the class of defect Gem had for months — **an adapter that returns something
while looking healthy**.

It is a companion to `docs/dedupe-audit.md`, which was written the same day
against the same crawl and owns everything URL-shaped: what each adapter puts in
`URL`, which adapters publish another vendor's link, and whether `internal.Dedupe`
should normalise. This one covers the rest of the posting — **truncation, pay
magnitude, publication dates, and tenant identity** — and does not restate its
findings.

Three defects were found and fixed. Six more are reported for a decision. Several
things that looked exactly like the signature turned out to be honest behaviour,
and are recorded as such, because a negative result nobody writes down gets
re-investigated every quarter.

## How it was measured

```console
$ job-hunter-toolkit postings --no-dedupe --csv \
    --csv-columns "source_platform,source_key,url,posted_at,pay_min,pay_max,currency,period,title,location" \
    --concurrency 8 --timeout 60m --stats > full.csv
1292271 postings from 8153 sources (10 sources failed)
```

`--no-dedupe` is the point: `internal.Dedupe` keys on URL, so it is exactly what
hides this defect class. A source that emits one URL many times looks like a
small healthy board afterwards and like a truncated one only before. Analysis was
per source and per platform in Python, never by eye. Where a defect was fixed,
the platform was re-crawled with the fixed binary and the two runs joined on URL.

**Corpus shape:** 1,292,271 postings before dedupe, 1,277,495 distinct URLs.

| platform | postings | sources returning postings | dated | with pay |
| --- | ---: | ---: | ---: | ---: |
| jibe | 414,314 | 247 | 76.9% | 6.8% |
| oraclecloud | 269,222 | 1,192 | 100% | 0% |
| workday | 175,286 | 208 | 65.9% | 0% |
| successfactors | 98,311 | 717 | 42.5% | 14.0% |
| brassring | 63,811 | 33 | 0% | 0% |
| smartrecruiters | 44,399 | 52 | 100% | 0% |
| greenhouse | 41,734 | 614 | 100% | 0% |
| radancy | 41,560 | 14 | 6.8% | 0% |
| breezy | 36,009 | 1,492 | 100% | 36.5% |
| teamtailor | 24,431 | 988 | 100% | 13.9% |
| phenom | 17,714 | 14 | 100% | 0% |
| ashby | 12,748 | 400 | 100% | 59.4% |
| personio | 11,924 | 968 | 100% | 10.0% |
| lever | 9,860 | 121 | 100% | 34.7% |
| recruitee | 9,832 | 489 | 100% | 34.1% |
| **workable** | 7,299 | 64 | **0%** | 0% |
| pinpoint | 6,400 | 117 | 0% | 46.7% |
| jobvite | 4,268 | 32 | 0% | 0% |
| **rippling** | **1,011** | 99 | 0% | 0% |
| gem | 756 | 44 | 100% | 0% |
| direct | 619 | 2 | 98.2% | 0% |
| peopleforce | 568 | 33 | 0% | 0% |
| bamboohr | 195 | 30 | 0% | 0% |

Every platform total matches `docs/measurements/2026-07-28-crawl.md` to within
the noise of a live board changing between two runs (jibe 414,318 → 414,314,
breezy 36,009 → 36,009, recruitee 9,832 → 9,832). **No platform has gone to
zero**, which was signature 3 on the list and is a clean negative.

---

# Fixed

## 1. Rippling truncated every board at 20 postings

**22 of the 99 Rippling boards that returned anything returned exactly 20.** That
is the shape of a cap, not of a coincidence, and the cap is the page the adapter
reads: `__NEXT_DATA__` embeds page 0 at a page size of 20, and the adapter
modelled only `items`.

The evidence was inside the payload the adapter already parses:

```console
$ curl -sS https://ats.rippling.com/aspenview/jobs | \
    grep -o '"totalItems":[0-9]*,"totalPages":[0-9]*'
"totalItems":70,"totalPages":4
```

Seventy openings; twenty read. `?page=N` serves the rest and is what the board's
own front end asks for.

**Fixed** in `internal/services/rippling.go`: `ripplingPage` fetches one page and
closes its body, the loop is bounded by the board's own `totalPages`, by
`ripplingMaxPages`, and by `pageRepeatGuard`. Regression test
`TestRipplingPaginates`.

Whole platform, re-crawled before and after:

| | before | after |
| --- | ---: | ---: |
| postings emitted | 1,011 | 1,397 |
| distinct URLs — what a reader actually sees | 908 | 1,397 |

**+54%.** `aspenview` went from 7 postings to 23.

## 2. Workable published no dates at all

`published_on` has been in the widget response since before the adapter was
written and **was never decoded**. Every Workable posting reached
`internal.Filter.PostedSince` with a zero `PostedAt`, and that filter excludes an
undated posting by design — so the entire platform was invisible to
`--posted-since`, silently, because an excluded posting looks exactly like a
company that is not hiring. This is the Phenom defect the brief describes,
reached by a different road: there a zone offset would not parse, here nobody
asked for the field.

```console
$ curl -sS https://apply.workable.com/api/v1/widget/accounts/datacom1 | jq '.jobs[0] | keys'
[ "application_url", "city", "code", "country", "created_at", "department",
  "education", "employment_type", "experience", "function", "industry",
  "locations", "published_on", "shortcode", "shortlink", "state",
  "telecommuting", "title", "url" ]
```

**Fixed** in `internal/services/workable.go`. `published_on` fills `PostedAt`;
`created_at` is when a requisition was drafted, which is earlier and is not what a
job seeker means by "posted", so it is decoded and deliberately unused.

The same commit merges Workable's one-entry-per-site fan-out
(`workableMergeSites`), which `docs/dedupe-audit.md` §5 measured at 4,691
postings that `Dedupe` was deleting — `kreyco` sends 4,878 entries for 914
openings, and only the first city of each survived. After the fix a six-city
opening reads all six, and `--location melbourne` can find a job in Melbourne.

Verified live:

```console
$ job-hunter-toolkit postings --json --no-dedupe --company kreyco --company datacom1
kreyco    914 postings, 914 distinct URLs, 914 dated   (was 4,878 rows -> 914 after dedupe, 0 dated)
datacom1  107 postings, 107 distinct URLs, 107 dated
```

`department` and `employment_type` are in the same response and still discarded.
Worth a follow-up; not a defect, so not done here.

## 3. Breezy never read the pay period, so a weekly rate was published as annual

Breezy publishes pay only as a rendered string, and always separates the figures
from the unit with a **spaced** slash: `$40 – $60 / day`. Every period marker in
`internal/compensation_text.go` is written without that space — `/hour`, `per
hour`, `hourly` — so **not one Breezy pay string in the registry ever set a
period**: 13,144 of 13,149 pay records arrived with `period` empty.

All of them fell through to the magnitude heuristic in
`Compensation.effectivePeriod`, which calls anything at or under 250 hourly and
anything above it annual. Right for hourly and annual strings, which are most of
the platform; wrong for the rest. Measured by fetching the board feed of all 774
Breezy tenants that publish pay — 14,335 salary strings:

| ending | count |
| --- | ---: |
| `/ hour` | 6,917 |
| `/ year` | 5,909 |
| `/ month` | 567 |
| `/ week` | 215 |
| `/ day` | 95 |

**Fixed** in `internal/services/breezy.go`: `breezyPeriodWording` rewrites the
spaced unit into wording the parser recognises, for the copy handed to the parser
only. `Summary` keeps the employer's own rendering. Regression test
`TestBreezyReadsTheSpacedPayPeriod`.

All 1,492 Breezy sources re-crawled, joined on URL:

- **3,482 postings now publish a pay range that used to be discarded.** A weekly
  or monthly range read as annual usually fell under the parser's
  `minPlausibleAnnual` and was thrown away, so the common outcome was not a wrong
  number but no number.
- **15 postings had a wrong figure corrected**: 12 monthly ranges published at
  1/12 of their value (`$100,000 – $120,000 / month` was published as $120,000 a
  year), and 3 day rates published at 8x (`$40 – $60 / day` as $124,800 a year) —
  the same failure on the same unit that `dailyMarkers` was added to
  `compensation_text.go` to stop in prose.
- **148 postings correctly lost a pay figure that had been invented.** Every one
  is a string whose stated period contradicts its magnitude, which the heuristic
  papered over: `$150 – $250 / year` was published as a **$312,000–$520,000**
  salary, and `$112,000 – $132,000 / hour` as $132,000 a year. All carried
  `ProvenanceEmployer`. Publishing nothing is the honest outcome.
- Unlabelled pay records fell from 13,144 to 610.

**The general fix is not here** — see finding 8.

---

# Reported, not fixed

## 4. Thirteen Workday tenants are silently truncated at exactly 2,000

Thirteen Workday tenants returned exactly 2,000 postings. The adapter derives its
page offsets from the `total` the tenant reports, and Workday clamps that number:

```console
$ post() { curl -sS -X POST -H 'Content-Type: application/json' \
    https://nvidia.wd5.myworkdayjobs.com/wday/cxs/nvidia/NVIDIAExternalCareerSite/jobs \
    -d "{\"appliedFacets\":{},\"limit\":1,\"offset\":0,\"searchText\":\"$1\"}" | jq .total; }
$ post ""           # 2000
$ post engineer     # 2000   <- same as unfiltered, so clamped
$ post CUDA         #  537
$ post marketing    #  771
$ post sales        #  292
$ post librarian    #    1
```

"engineer" alone reports the same 2,000 as the unfiltered search, while marketing
(771) and sales (292) are largely outside it — so NVIDIA's board is comfortably
larger than 2,000, and the crawl reports 2,000 as though it were complete.

Offsets past the window do not error, they **wrap**: offset 2000, 3000, 5000 and
10000 all return the same first posting as offset 0. `pageRepeatGuard` catches
that, which is why the crawl stops cleanly — and is exactly why the truncation is
silent. It looks like the end of a board.

```
aah.wd5/External                     adventhealth.wd12/AH_External_Career_Site
citi.wd5/2                           kohls.wd504/kohlscareers
massgeneralbrigham.wd1/MGBExternal   nvidia.wd5/NVIDIAExternalCareerSite
petco.wd504/External                 pnc.wd5/External
sysco.wd5/syscocareers               target.wd5/targetcareers
trinityhealth.wd1/Jobs               uhaul.wd1/UhaulJobs
wvumedicine.wd1/WVUH
```

Target and Sysco at exactly 2,000 is the clearest tell; both are six-figure
headcount employers.

This is the same shape as `radancyMaxWindow` and `oracleCloudMaxWindow`, both of
which are documented constants whose comments explain that yielding the window is
the correct outcome and erroring is not. Workday has no equivalent.
**Recommendation:** document the 2,000 window in `workday.go` the way those two
are, and treat faceted paging — by location or category, the way Workday's own UI
segments a large board — as the way past it. Neither is small, so neither is done
here.

## 5. Fifteen Oracle Cloud tenants are registered on non-production hosts

`docs/adding-a-source.md` already records that Oracle's own load-test tenant
nearly became the biggest employer in this project. Fifteen registered tenants
sit on hosts whose pod name says dev, test or stage:

```
bcci, govanbrown, layton, lfdriscoll,     fa-exrr-dev2-saasfaprod1.fa.ocs
  pavarinimcgovern
coniferhealthsolutions,                   eodr-dev5.fa.us2
  tenethealthcarecorporation,
  unitedsurgicalpartnersinternational
dtujobsside                               efzu-dev8.fa.em2
ehc                                       ibwsjb-dev2.fa.ocs
svkmcentraloffice, svkm...ndpar,          fa-elxu-test-saasfaprod1.fa.ocs
  svkmsjvparekhinternationalschool
usfuniversityofsouthflorida               fa-ewkd-dev1-saasfaprod1.fa.ocs
workingatsignatureaviation                hdbt-dev1.fa.us2
```

Four of them have the worst duplicate ratios in the registry — the shape of a
seeded, non-refreshed environment rather than a careers site:

| tenant | postings | distinct URLs |
| --- | ---: | ---: |
| `tenethealthcarecorporation` on `eodr-dev5` | 5,174 | **839** |
| `ehc` on `ibwsjb-dev2` | 1,928 | 1,622 |
| `coniferhealthsolutions` on `eodr-dev5` | 68 | **15** |
| `unitedsurgicalpartnersinternational` on `eodr-dev5` | 47 | 27 |

Note also that Tenet is **registered twice** — `tenethealth` on Radancy (3,350
postings) and `tenethealthcarecorporation` on Oracle Cloud — under two different
display names, so `reviewedDoubleCounts` in
`internal/services/double_count_test.go` never saw the collision. That test keys
on the company name, and these two names do not collide.

**Do not delete on this evidence.** `-dev` in an Oracle pod name is not proof —
the `saasfaprod1` suffix on several of them suggests production served from a
dev-named pod — and deleting a quiet source costs real coverage. The decision a
human should make: crawl `tenethealth` and `tenethealthcarecorporation` and
compare them posting-for-posting, then add the pair to `reviewedDoubleCounts`
with whatever verdict that produces.

## 6. Recruitee follows a redirect off the tenant it asked for

`docs/dedupe-audit.md` §5 records four pairs of Recruitee slugs that are aliases
for one board, and notes that Recruitee canonicalises the URL so `Dedupe`
collapses them harmlessly. This is the mechanism underneath that, and it is the
part worth fixing:

```console
$ curl -sS -o /dev/null -L -w '%{url_effective}\n' https://gainpro.recruitee.com/api/offers/
https://gain.recruitee.com/api/offers/
```

The dead tenant answers a redirect, the shared client follows it, and the adapter
parses **another tenant's feed and reports success** — the exact failure
`docs/adding-a-source.md` documents for Personio and BambooHR, whose general rule
is "compare the response's **final** URL host against the host you asked for
before parsing anything." Recruitee does not.

Today the cost is four phantom companies in `companies` rather than a double
count. It will not stay that way: with 492 registered Recruitee tenants, every
future death lands here. **Recommendation:** a final-URL host check in
`recruitee.go`, which is the platform-shaped fix and catches every future dead
tenant rather than these four.

## 7. Jobvite `splunk-careers` is a dead tenant that answers 200

```console
$ curl -sS -o /dev/null -L -w '%{http_code} %{url_effective}\n' \
    'https://jobs.jobvite.com/splunk-careers/search?nl=1&p=1'
200 https://www.jobvite.com/support/job-seeker-support/?invalid=1
```

All 33 Jobvite tenants were probed; this is the only one that produced nothing,
and it is not merely empty — it redirects off the tenant host onto the vendor's
support page. `jobviteDeprecated` looks for a `why-jobvite` link and does not
catch this shape. It publishes no bogus postings, so the cost is small, but every
crawl spends four retried requests on a vendor host with no `servicePolicyFor`
entry, which is the unpaced-request hazard the Personio note warns about. It
shows up in the crawl log as:

```
level=WARN msg="HTTP request failed after retries" url="http://search.jobvite.com?invalid=1" attempts=4
```

## 8. The shared pay parser does not recognise a spaced period unit

Finding 3 is the Breezy-local half of a general gap. `hourlyMarkers`,
`dailyMarkers` and `annualMarkers` in `internal/compensation_text.go` all spell
the unit without a space after the slash, so any board rendering `/ year` is read
as having stated no period at all.

| platform | pay records | no period | of those, above the 250 ceiling and therefore silently annualised at 1x |
| --- | ---: | ---: | ---: |
| jibe | 28,337 | 9,450 | 964 |
| successfactors | 13,764 | 7,227 | 5,475 |
| ashby | 7,575 | 1,057 | 1,048 |
| lever | 3,417 | 192 | 170 |
| recruitee | 3,351 | 185 | 174 |
| breezy (before fix 3) | 13,149 | 13,144 | 6,215 |

`internal/compensation_text.go` is not this audit's file, so this is a report
rather than a patch. Adding `"/ hour"`, `"/ year"`, `"/ month"`, `"/ week"` and
`"/ day"` to those lists is the whole change, and finding 3's before/after
numbers are a fair estimate of its value.

## 9. Four FedEx postings publish a minimum above their maximum

Corpus-wide there are exactly **four** postings whose pay minimum exceeds its
maximum. All are Jibe, all FedEx:

```
Warehouse Package Handler          28.98 – 26.09 /hour
Freight Handler Part-Time           2146 – 25.11 /hour
Retail Customer Service Associate  18.69 –  2.20 /hour
Retail Customer Service Associate  17.20 –  2.20 /hour
```

`jibeCompensation` copies `salary_min_value` and `salary_max_value` verbatim, so
this is the employer's data, not a parse error. Four rows in 1.29 million is not
worth code. Recorded so the next audit does not re-derive it, and so a swap-if-
inverted guard has a baseline to be judged against if the count ever grows.

---

# Checked and found honest

These looked like the signature and are not.

**Undated platforms are mostly undated at the source.** Of the seven platforms at
0%, only one was a defect:

| platform | why |
| --- | --- |
| brassring | deliberate; `brassRingTime` puts the only date on `UpdatedAt`, since editing a requisition does not make it new |
| pinpoint | deliberate and measured; the board publishes only `deadline_at` |
| bamboohr | `/careers/list` has no date field — verified live, the response carries 9 keys and none is a date |
| rippling | the dehydrated `job-posts` payload has 6 keys and none is a date |
| peopleforce | none modelled, none published |
| jobvite | reads a date opportunistically out of an unlabelled row cell; no registered tenant publishes one |
| **workable** | **a real defect — finding 2** |

**Radancy's 6.8% is a template difference, not a bug.** Three of 14 tenants
(Disney 798/798, Munich Re 1,517/1,517, Wegmans 499/499) date every posting; the
other 11 date none. The adapter reads a `job-date-posted` element and
`jobs.chipotle.com` does not render one — its result markup carries
`job-address`, `job-location` and `view-job`, and no date class at all.

**Workday's 34% undated is the same story.** `adventhealth`, `vumc`,
`adventisthealthcare`, `msmc` and `nationwide` omit `postedOn` from the `cxs`
response entirely. The field is not there to read.

**BambooHR's 45% silence is honest.** 25 of its 55 sources returned nothing, the
highest rate in the registry, and BambooHR is the platform `docs/adding-a-source.md`
warns answers a dead slug with a 302 to marketing. All 25 were probed directly:
every one answers HTTP 200 with `{"meta":{"totalCount":0},"result":[]}`. They are
companies that are not hiring.

**"Exactly one posting" is not the Gem signature here.** 324 Breezy sources, 105
Personio and 102 Teamtailor return exactly one. Every one is one real posting with
its own URL — the shape of a 1,492-source SMB platform where most tenants are a
five-person company with one opening. `docs/dedupe-audit.md` confirms the other
half independently: zero boards in the registry collapse to a single URL.

**Extreme pay figures are real currencies, not magnitude errors.** Every posting
above $2M annual is a JPY, INR, COP, HUF or CLP range with the correct currency
attached: Coupa's JPY salaries on Lever, Cartesia's INR salaries on Ashby,
`gerenciaselecta`'s monthly COP on Teamtailor. The plausibility bounds in
`compensation_text.go` are USD-calibrated and say so; a currency-aware bound would
be a real improvement, but nothing here is being reported wrongly.

**Round and identical counts are otherwise absent.** Beyond Rippling's 20,
Workday's 2,000 and Radancy's documented 10,000 window at Walgreens, no platform
shows a mode that looks like a cap. The most common per-source counts on the SMB
platforms are 2, 3 and 4.

**Only 10 of 8,153 sources failed**, and 183 returned nothing. The silence is
concentrated in the SMB platforms, and where it was probed it is empty boards
rather than dead ones.
