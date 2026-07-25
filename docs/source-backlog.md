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
| SurrealDB | <https://surrealdb.pinpointhq.com> | Hosted on Pinpoint, worth a shared `pinpointhq` service adapter. |
| Bending Spoons | <https://jobs.bendingspoons.com/> | |

## Preferred approach

Two of these (Memgraph, SurrealDB) are on shared ATS platforms, join.com and
Pinpoint. Adding those as `internal/services` adapters covers every other
company on the same platform for the same effort, which is the pattern that
makes the rest of this project scale. Prefer that over one-off company scrapers.

## Unsupported ATS platforms

The single biggest gap in coverage is not missing companies; it is missing
*platforms*. Large non-tech employers (hospital systems, grocery, rail, energy,
defense, airlines) mostly do not use the platforms supported today, which is why
those industries are under-represented.

These were identified by fingerprinting live careers sites, so the platform
attributions are evidence-based rather than guessed:

| Platform | Confirmed employers | Confidence |
| --- | --- | --- |
| **Phenom People** | Southwest Airlines, Lowe's | Confirmed (`cdn.phenompeople.com` assets) |
| **SAP SuccessFactors** | ExxonMobil | Confirmed (`rmkcdn.successfactors.com`) |
| **Oracle Taleo** | Kaiser Permanente | Confirmed (`kp.taleo.net/careersection`) |
| **Oracle Cloud HCM** | Mayo Clinic | Confirmed (`fa.ocs.oraclecloud.com/hcmUI/CandidateExperience`) |
| **IBM BrassRing / Kenexa** | Lockheed Martin, Home Depot (hourly roles) | Confirmed (`sjobs.brassring.com`) |
| **iCIMS proper** (no Jibe wrapper) | Charles Schwab, Costco | Confirmed |
| **Avature** | Ally Financial, Lockheed Martin (talent network) | Confirmed |
| **Radancy** | Kroger | Tentative, URL pattern only (`/sites/CX_NNNN`) |
| Proprietary | Walmart | Confirmed no third-party fingerprint; needs a bespoke scraper |

Phenom, SuccessFactors, and iCIMS-proper look like the highest-value additions:
each covers many very large employers, and one adapter unlocks all of them.

Also unresolved, worth a follow-up fingerprinting pass: Best Buy, Johns Hopkins
Medicine, Union Pacific, and most Class I freight rail and major airlines, none
were found on any currently supported platform.

## Known adapter limitations

- **Jobvite** is migrating tenants to a client-side-rendered careers template that
  the static HTML parser cannot read. Affected real, hiring tenants include ign,
  cdw, extremenetworks, carfax, asus, sitecore, and progress; they return zero
  postings because of the template, not because they are empty. A growing blind
  spot; these tenants were deliberately not added, since they would contribute
  nothing.
- **Eightfold** was removed entirely. Its public jobs API returns
  `{"message": "Not authorized for PCSX"}` even when replaying a real browser
  session's cookies, CSRF token, and Referer, an authorization wall, not
  something a User-Agent change defeats.
- **Workday** serves a maintenance redirect (to `/wday/drs/outage` or
  `community.workday.com/maintenance-page`) for tenants whose pod is down. That is
  indistinguishable from a dead tenant in the current error text, so a health
  check can misclassify a live board as broken. Re-check apparent Workday failures
  on a weekday before pruning.
- **BambooHR** answers an unknown tenant with a 302 to its marketing homepage, so
  a dead slug surfaces as a JSON decode error (`invalid character '<'`) rather
  than a 404. That is the expected signature for a dead BambooHR slug.
