# Source backlog

Companies whose careers pages are not yet wired up. These were previously
tracked as empty placeholder `.go` files in `internal/companies`; they live here
instead so the package only contains code that actually runs.

Each of these runs its own careers site rather than a supported ATS, so each
needs a bespoke adapter (like `internal/companies/oxide.go`) and its own
fixture-backed test.

| Company | Careers endpoint | Notes |
| --- | --- | --- |
| Amazon | <https://www.amazon.jobs> | Very large; needs pagination and rate-limit care. |
| DISH | <https://careers.dish.com/> | |
| McDonald's | <https://jobs.mchire.com/jobs?page_size=10&page_number=1&sort_by=headline&sort_order=ASC> | JSON API with explicit paging params. |
| Memgraph | <https://join.com/companies/memgraph> | Hosted on join.com, worth a shared `join.com` service adapter instead. |
| PlanetScale | <https://planetscale.com/careers> | |
| ~~SurrealDB~~ | <https://surrealdb.pinpointhq.com> | **Done.** The `pinpoint` adapter covers it and 33 other tenants. |
| Bending Spoons | <https://jobs.bendingspoons.com/> | |

## Preferred approach

This worked, and it is the reason SurrealDB is struck above. Both entries that
were on a shared platform justified a service adapter instead of a company
scraper: Pinpoint is now an `internal/services` adapter carrying 34 tenants,
where a SurrealDB scraper would have carried one. join.com is the same argument
still unspent, and Memgraph is still the only reason to make it.

Prefer a service adapter over a one-off company scraper wherever the choice
exists. It is the pattern that makes the rest of this project scale.

## Unsupported ATS platforms

The single biggest gap in coverage is not missing companies; it is missing
*platforms*. Large non-tech employers (hospital systems, grocery, rail, energy,
defense, airlines) mostly do not use the platforms supported today, which is why
those industries are under-represented.

These were identified by fingerprinting live careers sites, so the platform
attributions are evidence-based rather than guessed:

| Platform | Confirmed employers | Status |
| --- | --- | --- |
| ~~**Phenom People**~~ | Southwest Airlines, Lowe's | **Done**, 15 tenants registered. |
| ~~**SAP SuccessFactors**~~ | ExxonMobil | **Done**, 30 tenants registered, 744 staged. |
| **Oracle Taleo** | Kaiser Permanente | Still missing (`kp.taleo.net/careersection`). |
| ~~**Oracle Cloud HCM**~~ | Mayo Clinic | **Done** as `oraclecloud`, 30 tenants registered, 1,552 staged. |
| **IBM BrassRing / Kenexa** | Lockheed Martin, Home Depot (hourly roles) | Still missing (`sjobs.brassring.com`). |
| **iCIMS proper** (no Jibe wrapper) | Charles Schwab, Costco | Still missing. The Jibe-wrapped variant is covered: 127 tenants. |
| **Avature** | Ally Financial, Lockheed Martin (talent network) | Still missing. |
| ~~**Radancy**~~ | Kroger | **Misattributed.** `/sites/CX_NNNN` is Oracle Recruiting Cloud's own URL shape, not Radancy's. Kroger is staged as the Oracle Cloud tenant `kroger,eluq.fa.us2.oraclecloud.com,CX_2001`, the largest candidate in that file at roughly 16,300 postings. There is no evidence left for a Radancy adapter. |
| Proprietary | Walmart | Confirmed no third-party fingerprint; needs a bespoke scraper. |

Two of the three "highest-value additions" this table used to name — Phenom and
SuccessFactors — are now adapters. **iCIMS proper is the remaining one**, and
BrassRing and Avature are the next largest gaps: each covers many very large
employers, and one adapter unlocks all of them.

Platforms added since this table was written, none of which it anticipated:
Recruitee (35 registered), Teamtailor (34), Pinpoint (34), Personio (37), plus
the Jibe vanity-host variant, plus Eightfold (21 registered). The registry now
spans 20 applicant tracking systems and 2,208 sources.

Also unresolved, worth a follow-up fingerprinting pass: Best Buy, Johns Hopkins
Medicine, Union Pacific, and most Class I freight rail and major airlines, none
were found on any currently supported platform.

## Staged candidates

A research pass recovered far more tenants per platform than were registered.
None of them could be probed: the container that found them has no outbound
access to a job board. They are staged, unregistered, one per line with
provenance headers, under `internal/services/testdata/candidates/`:

| File | Candidates | Registered | Open |
| --- | ---: | ---: | ---: |
| `oracle_orc_tenants.txt` | 1,552 | 30 | 1,522 |
| `teamtailor_slugs.txt` | 1,037 | 34 | 1,003 |
| `personio_slugs.txt` | 999 | 37 | 962 |
| `successfactors_tenants.txt` | 744 | 30 | 714 |
| `recruitee_slugs.txt` | 507 | 35 | 472 |
| `pinpoint_tenants.txt` | 119 | 33 | 86 |
| `jibe_vanity_hosts.txt` | 206 | — | 206 |

The six platform files hold 4,958 candidates between them; the Jibe file adds
206 employer-owned career hostnames beyond the 127 already registered.

**Why they are not registered.** An unverified slug is not free. A dead one
costs four retry attempts against a shared backend on every nightly crawl, and
a live one nobody expected can be enormous — the largest staged Oracle tenant
alone claims roughly 16,300 postings, which is a third of a crawl budget for a
company nobody chose. Registering ~4,958 unprobed slugs would multiply the
registry by three on the strength of a directory listing.

**How to promote one.** Dispatch
`.github/workflows/verify_candidates.yml` with a platform, an `offset` and a
`limit`. It stages exactly that batch into the registry *inside the runner's
working copy*, checks it with the same `health` command, HTTP client and rate
limiter a crawl uses, and reports which slugs answered. It runs with
`contents: read` and cannot commit; it prints the next offset so the list can
be walked one bounded batch at a time. Promotion is a human editing the
registry afterwards.

Read the report carefully. `ok` means the board answered with postings and is
promotable. `empty` is **not** evidence: a company that is not hiring and a dead
tenant whose ATS answers 200 with an empty list look identical from one sample,
and the adapter limitations below are a list of platforms whose idea of
"missing" is not a 404. The registry tests assert that every registered tenant
appears in its candidate file, so a promotion that skips the file will fail CI.

## Known adapter limitations

- **Jobvite** is migrating tenants to a client-side-rendered careers template that
  the static HTML parser cannot read. Affected real, hiring tenants include ign,
  cdw, extremenetworks, carfax, asus, sitecore, and progress; they return zero
  postings because of the template, not because they are empty. A growing blind
  spot; these tenants were deliberately not added, since they would contribute
  nothing.
- **Eightfold** gates its list API per tenant, and only a minority of tenants
  leave it open. A gated tenant answers `HTTP 403` with
  `{"message": "Not authorized for PCSX"}`, and that wall is not something a
  User-Agent change, a proxy, or a replayed browser session defeats — cookies,
  CSRF token and Referer were all tried. What the earlier note got wrong is that
  the wall is **per-tenant, not platform-wide**: of 133 live tenants probed,
  21 answered with postings and are registered in `internal/services/eightfold.go`,
  109 are gated, and 3 answer but publish nothing. It depends on neither the
  `domain` query parameter nor the branded careers host; the same slug answers
  the same way through `jobs.<employer>.com` as through `eightfold.ai`. The full
  probed list, with each tenant's answer, is
  `internal/services/testdata/candidates/eightfold_slugs.txt`. The route that
  would reach the gated 109 is the sitemap at
  `{slug}.eightfold.ai/careers/sitemap.xml` plus the schema.org JSON-LD on each
  job page, at roughly one request per posting; that stays deferred behind the
  shared `jsonld.go` helper `docs/research/ats-platform-survey.md` recommends.
  The list API is also the slowest-paging platform here: it caps a page at ten
  postings whatever `num` asks for.
- **Workday** serves a maintenance redirect (to `/wday/drs/outage` or
  `community.workday.com/maintenance-page`) for tenants whose pod is down. That is
  indistinguishable from a dead tenant in the current error text, so a health
  check can misclassify a live board as broken. Re-check apparent Workday failures
  on a weekday before pruning.
- **BambooHR** answers an unknown tenant with a 302 to its marketing homepage, so
  a dead slug surfaces as a JSON decode error (`invalid character '<'`) rather
  than a 404. That is the expected signature for a dead BambooHR slug.
