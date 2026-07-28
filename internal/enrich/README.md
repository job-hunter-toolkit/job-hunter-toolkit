# Enrichment tables

What is known about the *company* behind a job board, as opposed to what a board
published about a job. It is what turns a list of openings into something you can
do market research with: industry, public/private, headcount, founding year,
headquarters, parent company, and eventually wage benchmarks.

Everything here is a **generated, reviewed, committed table** that the binary
embeds with `go:embed` and reads with zero request cost. Nothing in the CLI's
import graph can make a network call for enrichment.

## Why it is not a live lookup

The crawl measured on 2026-07-26 recorded `07/26/26 473404 1772 partial` —
473,404 postings from 1,772 companies, and it did not finish inside 350 minutes
on GitHub Actions. Adding one third-party lookup per company would add ~2,131
requests to a run that already misses its budget, and SEC EDGAR caps every client
at 10 requests per second across all of its hosts, so those requests cannot be
parallelised out of the way either.

Embedding also keeps the promise `docs/architecture-roadmap.md` makes about the
default binary: no CGO, no required state, no daemon.

## What is committed

| File | Written by | Embedded | Contents |
| --- | --- | --- | --- |
| `data/employers.tsv` | the generator | yes | one row per crawled source, overwritten wholesale on every run |
| `data/manual.tsv` | humans | yes | corrections and hand-confirmed matches, overlaid on top of the generated file |
| `data/wages.tsv` | (no generator yet) | yes | wage benchmark distributions; schema only, empty |
| `data/candidates.tsv` | the generator | no | the review queue: matches that were refused, and why |

**Both employer tables are currently header-only.** No generator run against the
live sources has happened: it needs outbound network access, which only GitHub
Actions has. Rows will not be invented to make the files look populated — a
fabricated CIK or headcount is indistinguishable from a real one once it is in
the table, which is exactly the failure this design exists to prevent.

## Sources, licences, attribution

| Source | Used for | Licence | Access policy |
| --- | --- | --- | --- |
| [SEC EDGAR](https://www.sec.gov/search-filings/edgar-application-programming-interfaces) `company_tickers.json` + `submissions` | legal name, CIK, ticker, exchange, SIC industry, business address, public status | US Government work, public domain | User-Agent must name a reachable contact; 10 req/s across **all** sec.gov hosts, enforced with a 403 and a ~10 minute IP block |
| [Wikidata](https://query.wikidata.org/) SPARQL | employees (P1128), inception (P571), industry (P452), headquarters (P159), parent (P749) | CC0, no attribution required | Generic user agents are rejected; 60s query time per minute, 5 concurrent per IP, 429 escalates to a ban |

Planned, not built (see `data/wages.tsv`): DOL OFLC LCA/PERM disclosure files and
BLS OEWS flat files, both public domain, both aggregated offline into compact
distributions and never shipped raw. If O\*NET's database is ever used for
title→SOC matching, note that it is **CC BY 4.0** and requires an attribution
line in the README and `--version`.

## Regenerating

```sh
go run ./tools/enrichgen -out internal/enrich/data -contact ops@example.com
```

The contact is mandatory and the generator refuses to run without one. It can
also come from `JHT_ENRICH_CONTACT`, which is how a workflow should supply it so
it does not end up in a command line in a log.

The run writes `employers.tsv` and `candidates.tsv` and touches nothing else. It
refuses to write anything at all if most of its SEC fetches failed, because that
is the shape of an IP block and the resulting table would look like data while
saying "industry unknown" for every company.

Suggested workflow (`.github/workflows/enrichment.yml`), deliberately separate
from `track_jobs.yml` so a refresh can never contribute to the nightly crawl's
timeout:

```yaml
name: Refresh enrichment tables

on:
  schedule:
  - cron: "0 6 1 * *"   # monthly: SIC and state essentially never change
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  refresh:
    runs-on: ubuntu-latest
    timeout-minutes: 45
    steps:
    - uses: actions/checkout@v5
    - uses: actions/setup-go@v6
      with:
        go-version-file: go.mod
    - name: Regenerate
      env:
        JHT_ENRICH_CONTACT: ${{ vars.ENRICHMENT_CONTACT }}
      run: go run ./tools/enrichgen -out internal/enrich/data
    - name: Verify the binary still loads what was written
      run: go test ./internal/enrich/...
    - uses: peter-evans/create-pull-request@v7
      with:
        branch: enrichment-refresh
        title: "Refresh enrichment tables"
        body: |
          Regenerated from SEC EDGAR and Wikidata. Coverage is in the run log.
          Review `data/candidates.tsv` for matches that were refused.
```

## Reviewing

The generator commits **only** matches that are unique in both directions: the
crawled source proposes exactly one filer, and every other source proposing that
filer is the same company (a company on Greenhouse *and* on Workday is still one
company). Everything else goes to `candidates.tsv` with the reason.

To accept a candidate:

1. Confirm it by hand — open the filer on EDGAR and check it is the same company,
   not a similarly named one.
2. Add a row for it to `data/manual.tsv` with `match_method=manual` and
   `match_confidence=high`, and fill in `data_sources` and `retrieved`.

Do not paste rows into `employers.tsv`; the next run overwrites that file.
`manual.tsv` is overlaid on top of it at load time, which is what makes a
correction survive a refresh.

**Unmatched is a correct answer.** Expect low coverage: most companies here are
private startups on Greenhouse, Ashby and Lever, and EDGAR by definition knows
nothing about them. Coverage is printed next to every answer for that reason.

## Wage benchmarks are not pay

`WageBenchmark` exists in the model and in `data/wages.tsv`'s schema, and it is
deliberately not `internal.Compensation`. `docs/compensation.md` states that
nothing blends sources; `JobPosting.Compensation` is the range **this employer
published with this posting**. A DOL benchmark is a statutory wage an employer
certified on somebody else's visa application, and a BLS benchmark is a survey
estimate about an occupation in a metro. Neither may be written into
`Compensation`, and neither may satisfy `--min-pay`.
