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
| `data/manual.tsv` | humans | yes | corrections and hand-confirmed matches, applied on top of the generated file |
| `data/wages.tsv` | (no generator yet) | yes | wage benchmark distributions; schema only, empty |
| `data/candidates.tsv` | the generator | no | the review queue: matches that were refused, and why |

`employers.tsv` was regenerated from live sources on **2026-07-28**: 254 matches
from 8,173 crawled sources, 8,017 SEC filers seen (3,415 of them with a
corroborating website in Wikidata), 0 failed submission fetches, 1 blank-check
shell refused, 22 candidates for review, 174 rows decorated from Wikidata.
`manual.tsv` now carries 7 rows: one confirmed match promoted out of the review
queue, and six refutations. The binary serves 249.

Rows are never invented to make the files look populated — a fabricated CIK or
headcount is indistinguishable from a real one once it is in the table, which is
exactly the failure this design exists to prevent.

## What "unique" turned out not to be worth

The first live run committed 248 matches and a hand audit found ten wrong. Every
one of them was *unique in both directions* — the rule the resolver was built
around — and every one was a name collision: an acronym (`wf`, `mks`, `nmi`,
`esg`) or an ordinary word (`team`, `post`, `glow`, `citizens`) spelled the same
as a filer.

The second run, against a registry that had grown to 8,173 sources, proposed 263
matches under the old rule. Auditing those against the live boards found **14
wrong (94.7% precision)** — the ten from the first audit minus `team`, which had
become contested and dropped out on its own, plus five the first audit never
covered:

| Source | Matched to | Actually | How it was settled |
| --- | --- | --- | --- |
| `breezy` `authentic` | Authentic Holdings (OTC apparel) | a paid-media agency at authentic.org | the board's own link |
| `breezy` `giftify` | GIFTIFY, INC. (Chicago) | giftify.me, Brussels | the board's own link |
| `teamtailor` `waterdrop-1732181682` | Waterdrop Inc. (WDH, Chinese insurtech) | waterdrop®, the Austrian microdrink brand | the board's title |
| `greenhouse` `eve` | Eve Holding (EVEX, Embraer eVTOL, Melbourne FL) | Eve, legal software, San Mateo CA | the board's postings |
| `oraclecloud` `chs,fa-evxo…` | CHS INC (agricultural co-op, MN) | Community Health Systems — the postings are RN and respiratory-therapist roles | the tenant's postings |

So the pattern is not "acronyms and English words". It is **name equality with
nothing behind it**, and the audit's own framing was too narrow: `authentic`,
`giftify` and `waterdrop` are none of those things.

### What the rule is now

Three measurements decided it, all against the 263 matches of the second run.

**1. Short one-word names, corroborated or refused.** A match whose filer name
reduces to a single word shorter than five characters is committed only if an
identifier the filer *owns* agrees with that word: its trading symbol, or an
official website (Wikidata P856) reached by CIK (P5531) whose host carries that
label. The thresholds were not guessed:

| rule | wrong matches removed | correct matches lost |
| --- | --- | --- |
| every one-word name needs corroboration | 13 of 14 | 79 |
| one-word, ≤ 6 characters | 7 | 28 |
| one-word, ≤ 5 characters | 7 | 12 |
| **one-word, ≤ 4 characters** (chosen) | **7** | **1** |

Corroboration by ticker is knowingly allowed to rescue one wrong match: POST
really is Post Holdings' symbol. Dropping the ticker signal would catch it and
cost `rh`, `chs`… — twenty-odd correct short matches whose name *is* their
market identity. The one wrong row is refuted by hand instead.

**2. Blank-check shells are never employers.** A filer whose SIC is 6770 is a
listing waiting for a merger. One match had it (`personio` `dynamix`, a SPAC) and
no correct match did.

**3. The English-word test was measured and rejected.** Scoring the one-word
names longer than four characters against the 500, 1,000, 2,000, 3,000, 5,000 and
10,000 most frequent English words, and requiring a matching website for any that
hit:

| word list | wrong removed | right lost |
| --- | --- | --- |
| top 1,000 | 0 | 0 |
| top 2,000 | 0 | 1 (`block`) |
| top 5,000 | 1 (`citizens`) | 3 |
| top 10,000 | 2 | 9 |

It never pays. The correct matches contain as many ordinary English words as the
wrong ones — Block, Booking, Crown, Carnival, Rogers, Expedia, Reliance — and
buying `citizens` costs three of them, on top of shipping a dictionary in the
repo to do it.

Measured result: **263 matches with 14 wrong (94.7%) became 254 with 6 wrong
(97.6%)**. Nine rows moved to `candidates.tsv`; eight of them were wrong and one
(`smartrecruiters` `Wise`) was right and has been confirmed by hand and promoted
into `manual.tsv`.

### The six that still get through

`post`, `citizens`, `sinclair`, `authentic`, `giftify` and `waterdrop` all pass
every rule above: their names are long enough to look chosen, and their filers
are real operating companies. They are **refuted in `manual.tsv`**, so the binary
serves nothing for those six sources — but the generator will re-propose them on
every run, and `manual.tsv` is the only thing standing between them and the
table.

What would actually close the gap is the signal that settled all five of the new
audit findings by hand: **the domain the board itself links to**. Every ATS
publishes it somewhere — Breezy in the board's HTML, Teamtailor in the site
title, Greenhouse in the postings — and comparing it to the filer's own domain
turned each of those cases in under a minute. Nothing in the pipeline fetches it
today; the generator has never made a request to a job board. That is the next
piece of work, and it is worth more than any further tuning of the name rules.

## Sources, licences, attribution

| Source | Used for | Licence | Access policy |
| --- | --- | --- | --- |
| [SEC EDGAR](https://www.sec.gov/search-filings/edgar-application-programming-interfaces) `company_tickers.json` + `submissions` | legal name, CIK, ticker, exchange, SIC industry, business address, public status | US Government work, public domain | User-Agent must name a reachable contact; 10 req/s across **all** sec.gov hosts, enforced with a 403 and a ~10 minute IP block |
| [Wikidata](https://query.wikidata.org/) SPARQL | employees (P1128), inception (P571), industry (P452), headquarters (P159), parent (P749), and official website (P856) as corroboration for a short name | CC0, no attribution required | Generic user agents are rejected; 60s query time per minute, 5 concurrent per IP, 429 escalates to a ban |

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

**The contact must be an email address, not this project's repository URL.**
Measured against `data.sec.gov` on 2026-07-28: EDGAR answers 403 "Your Request
Originates from an Undeclared Automated Tool" to *any* User-Agent containing the
string `github`, and 200 to the same header with `gitlab.com` or `example.com`
substituted. The generator used to build its agent by appending the contact to
`httpx.DefaultUserAgent`, which embeds this project's GitHub URL, so every EDGAR
request it made was refused — which is why the tables stayed empty even though
the workflow above had network access. `fetch.UserAgent` now sends its own
product token and rejects a contact containing `github` up front.

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

The generator commits **only** matches that are unique in both directions — the
crawled source proposes exactly one filer, and every other source proposing that
filer is the same company (a company on Greenhouse *and* on Workday is still one
company) — *and* rest on a name long enough to identify somebody, or on a short
name an identifier corroborates. Everything else goes to `candidates.tsv` with
the reason.

To accept a candidate:

1. Confirm it by hand — open the board, find the company's own website, and check
   it against the filer on EDGAR. A board that links to `sinclair.com` is not the
   filer that publishes `sbgi.net`, however the two names are spelled.
2. Add a row for it to `data/manual.tsv` with `match_method=manual` and
   `match_confidence=high`, and fill in `data_sources` and `retrieved`.

To reject a committed row, add a row for it to `manual.tsv` with
`match_method=refuted` and every fact column empty. The loader **deletes** the
generated row rather than overlaying it, so the source serves no employer and is
not counted as covered.

Do not edit `employers.tsv`; the next run overwrites that file. `manual.tsv` is
applied on top of it at load time, which is what makes a correction survive a
refresh.

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
