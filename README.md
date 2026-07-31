# Job Hunter Toolkit

[![GitHub license](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/blob/master/LICENSE)
[![CI](https://github.com/job-hunter-toolkit/job-hunter-toolkit/actions/workflows/ci.yml/badge.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/actions/workflows/ci.yml)
[![go report](https://goreportcard.com/badge/github.com/job-hunter-toolkit/job-hunter-toolkit)](https://goreportcard.com/report/github.com/job-hunter-toolkit/job-hunter-toolkit)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/pulls)

The job hunter's toolkit. Searches the job boards of nearly 8,000 companies
directly, across 22 applicant tracking systems, and prints the results as text,
JSON, or CSV.

It talks to the same public board APIs that companies' own careers pages use, so
postings come from the source rather than from an aggregator. Coverage spans
hospital systems, universities, retailers, manufacturers, and banks as well as
tech companies, and reaches the hourly and skilled-trade work most job sites
index poorly: Home Depot alone publishes 22,899 store and hourly roles through
BrassRing against 972 corporate roles through Workday.

A full crawl measured on 2026-07-28 returned **1,236,756 postings from 7,931
companies in under sixteen minutes**, with 8,136 of 8,145 sources completing.
See [docs/measurements](docs/measurements/2026-07-28-crawl.md) for the method and
the per-platform costs.

## Install

With a Go toolchain installed:

```console
$ go install github.com/job-hunter-toolkit/job-hunter-toolkit@latest
```

## Usage

```console
$ job-hunter-toolkit --help
The job hunter's toolkit. Searches the job boards of more than a
thousand companies across the major applicant tracking systems.

Usage:
  job-hunter-toolkit [command]

Available Commands:
  companies   List the companies that postings are searched from
  health      Check every job source and report which ones are broken
  postings    Find job postings from various companies
  total       Count the job postings currently available
```

### Finding postings

Filters are the point: a full crawl returns over 1.2 million postings, which is
only useful once you can narrow it.

```console
$ job-hunter-toolkit postings --remote --title "security engineer"
company: vercel title: Security Engineer, Cloud location: Remote - United States url: https://...
company: wrike title: Senior Security Engineer location: Estonia - Remote url: https://...
...
```

Values within a flag are OR-ed; different flags are AND-ed. Matching is
case-insensitive substring matching against the text the board publishes.

| Flag | Effect |
| --- | --- |
| `--title` | only postings whose title contains any of these terms |
| `--exclude-title` | skip postings whose title contains any of these terms |
| `--location` | only postings whose location contains any of these terms |
| `--company` | only these companies; this narrows the crawl itself, so it is fast |
| `--remote` | only postings that look remote |
| `--has-pay` | only postings that publish a pay range |
| `--min-pay` | only postings paying at least this much per year (hourly rates are annualized) |
| `--department` | only postings whose department or team contains any of these terms |
| `--employment-type` | `full_time`, `part_time`, `contract`, `internship`, `temporary`, `volunteer`; board spellings like `Full-Time` are accepted |
| `--workplace-type` | `remote`, `hybrid`, or `onsite` |
| `--posted-since` | only postings published since a date (`2026-01-31`) or an age (`7d`, `2w`, `72h`) |
| `--json` / `--csv` | machine-readable output |
| `--csv-columns` | `core` (the frozen 8 columns), `extended`, or an explicit list |
| `--csv-header` | write a header row |
| `--stats` | print a summary to stderr when the crawl finishes |
| `--concurrency` | how many sources to fetch at once |
| `--timeout` | overall time budget |
| `--proxy` | optional HTTP, HTTPS, or SOCKS5 proxy; repeat to distribute boards across a pool |

Because `--company` narrows which boards are fetched, targeted queries finish in
about a second:

```console
$ job-hunter-toolkit postings --company anthropic --stats
...
461 postings from 2 sources (0 sources failed)
```

Nurses, teachers, and machinists are covered as much as engineers; the crawl
includes hospital systems, universities, retailers, and manufacturers alongside
tech companies:

```console
$ job-hunter-toolkit postings --title "registered nurse" --location "St. Louis"
$ job-hunter-toolkit postings --title teacher --location Texas
```

Postings are de-duplicated by URL by default, since a company can appear under
more than one board slug. Use `--no-dedupe` to see the raw stream.

### Pay

Where an employer publishes a pay range, it is parsed into a structured field:

```console
$ job-hunter-toolkit postings --company harvey --min-pay 200000
company: harvey title: Staff Software Engineer, Model Infrastructure location: San Francisco pay: $236K – $290K • Offers Equity • Offers Bonus url: https://...

$ job-hunter-toolkit postings --company petsmart --min-pay 60000
company: petsmart title: Pet Groomer location: Signal Hill, California pay: 17.17-29.95/hour url: https://...
```

Hourly rates are annualized (2080 hours) so one `--min-pay` figure works across
salaried and hourly roles alike.

**Coverage is uneven, and absence never means unpaid.** Only some platforms
publish pay as structured data at all:

| Platform | Pay data |
| --- | --- |
| Jibe / iCIMS | Yes, amounts plus an explicit frequency, populated on most postings |
| Ashby | Yes, amounts, currency, and interval, where the company opts in |
| Lever | Yes, but populated on only a fraction of postings |
| Greenhouse, Workday, SmartRecruiters, others | No structured field; pay appears only in the description |

Every pay figure records where it came from, because a wrong salary looks exactly
like a right one:

```console
$ job-hunter-toolkit postings --company harvey --has-pay --json | jq '.compensation.provenance' | sort | uniq -c
```

`employer` means the platform published it in a real API field; `structured`
means it came from markup a board renders from a pay field; `description` means
it was read out of prose and is the only kind that can be wrong about what a
number means.

Because a pay floor cannot be applied to an undisclosed salary, `--min-pay`
excludes postings that publish nothing; which is most postings, on most boards.
Use `--has-pay` to see exactly which ones disclose.

Reading pay out of description prose is implemented and tested but not yet run by
the crawl, since fetching descriptions inflates Greenhouse responses ~13.7x. See
[docs/compensation.md](docs/compensation.md) for the extraction rules, the
measured accuracy, and why that is behind a future opt-in flag.

Output is designed to be piped; diagnostics go to stderr, data to stdout:

```console
$ job-hunter-toolkit postings --remote --title appsec --json | jq -r '.url'
```

### Network behavior and proxies

The crawler interleaves ATS platforms and applies service-aware concurrency,
pacing, retries, and shared cooldowns. A strict shared endpoint does not have to
run at the same rate as tenant-isolated Workday hosts, and a 429 from one
multi-tenant backend delays sibling requests rather than triggering a retry
storm. Debug logs show the service key, retry delay, and the server's raw
`Retry-After` value:

```console
$ job-hunter-toolkit health --log-level debug
```

Go's standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` environment variables
work automatically. Explicit proxies can also be supplied; repeat the flag to
form a pool:

```console
$ job-hunter-toolkit total \
    --proxy https://proxy-a.example:8443 \
    --proxy socks5://proxy-b.example:1080
```

Proxy selection is deterministic and sticky per job board, so all pagination
and retries for one source stay on one route while different boards are spread
across the pool. The crawler does not automatically fail over a request to a
different proxy: that can duplicate a replayed request and conceal a broken
route. Proxy credentials are never logged. Prefer environment variables or a
secret-injection mechanism over putting credentials in command history.

Proxies are optional routing infrastructure, not a substitute for respectful
request rates. The crawler still honors the same per-service controls when a
pool is configured, and TLS verification remains enabled.

### Listing companies

```console
$ job-hunter-toolkit companies | wc -l
```

### Checking source health

Job boards get retired constantly, and a crawl that silently skips failures
slowly stops covering the companies it claims to. `health` makes that visible:

```console
$ job-hunter-toolkit health --failed-only
failed   somecompany    unexpected status code from Greenhouse for "somecompany": 404 Not Found
...

<total> sources: <n> ok, <n> empty, <n> failed
```

`empty` means the board is reachable but has no openings; that is a company not
hiring, not a broken source. Only `failed` is a problem.

Counting stops at 100 postings per source, reported as `100+`. A health check only
needs to know whether a source works, and some employers are enormous, FedEx
alone publishes over 138,000 postings, which is more than a thousand paginated
requests for a single company.

Narrow it to one company while adding a source:

```console
$ job-hunter-toolkit health --company newcompany
ok       newcompany     42 postings
```

## Supported job boards

One adapter per applicant tracking system, so adding a company is usually a
one-line change rather than a new scraper.

| Platform | Notes |
| --- | --- |
| Greenhouse | Largest source; common with tech companies |
| Ashby | Common with AI labs, developer tools, and startups; publishes structured pay |
| Workday | Dominates large enterprises, hospitals, and universities |
| Jibe / iCIMS | Large retail, grocery, restaurant, and health systems; best pay coverage |
| Phenom People | Large retail, industrial, and healthcare employers |
| Lever | Public v0 API; being deprecated upstream |
| Rippling | Startups using Rippling for HR |
| SmartRecruiters | Enterprise, strong in Europe |
| BambooHR | Small and mid-size US companies, nonprofits |
| Workable | Small and mid-size companies |
| Gem | |
| Jobvite | Mid-size and large enterprises |
| PeopleForce | Popular in Europe |
| SAP SuccessFactors | Very large enterprises; one request returns a whole employer's openings |
| Oracle Cloud HCM | Large enterprises, hospitals, and universities |
| Teamtailor | Small and mid-size companies, strong in the Nordics |
| Personio | Small and mid-size companies, strong in Europe |
| Recruitee | Small and mid-size companies |
| Pinpoint | Small and mid-size companies |
| Eightfold | Large global enterprises; only the minority of tenants that leave the list API ungated |

The six platforms between PeopleForce and Eightfold were added from documented
endpoint shapes rather than from boards this project has crawled, so each
registers a small, deliberately conservative set of tenants. Several thousand
further candidate tenants are staged, unregistered, in
`internal/services/testdata/candidates/`; they are promoted only once a live
`health` run confirms them, because a tenant that 404s is indistinguishable in
aggregate from an adapter that never worked.

Eightfold is the exception to the shape of that list, and worth knowing about
before you go looking for a company there: it gates the list API per tenant, so
most Eightfold employers refuse it outright with `HTTP 403`. Every tenant
registered here answered with postings on two separate runs, and the ones that
did not are recorded, with the answer each gave, in
`internal/services/testdata/candidates/eightfold_slugs.txt`.

A source is identified two ways: the **key** its platform uses to fetch it (a
board slug, a Workday tenant URL, a Phenom hostname) and the readable **company
name** derived from it. `--company` accepts either, and `health` shows both when
they differ, so a failure is actionable.

Company-specific adapters live in `internal/companies` for employers that run
their own careers site rather than a supported ATS. Prefer adding a platform
adapter over a one-off scraper: it covers every other company on that platform
for the same effort. See [docs/source-backlog.md](docs/source-backlog.md).

## Total Job Postings Over Time

A scheduled workflow records the total daily. **The level of this series tracks
the crawler's coverage and health at least as much as it tracks hiring**: it has
two multi-month gaps where the workflow broke silently, and step changes where
sources were pruned or coverage widened. See [docs/jobs-record.md](docs/jobs-record.md)
before drawing conclusions from it.

<img alt="Total Job Postings Over Time" src="https://raw.githubusercontent.com/job-hunter-toolkit/job-hunter-toolkit/master/jobs_record.png" height="500" />

## Development

```console
$ go build ./...
$ go test ./...
```

The staged direction for observable crawl sharding, historical storage, TUI,
MCP, and optional service operation is documented in
[docs/architecture-roadmap.md](docs/architecture-roadmap.md).

### How a crawl behaves

Each company is an independently scheduled source, fetched concurrently up to
`--concurrency`. Sources are interleaved across ATS platforms, then
`internal/httpx` applies service-aware concurrency and pacing. Shared backends
such as Workable and PeopleForce are grouped even when their tenant URLs differ;
tenant-isolated Workday hosts remain independent. This keeps one strict service
from monopolizing the worker pool or turning self-inflicted HTTP 429s into
apparently dead boards.

The client retries transient failures (5xx, 429) with jittered backoff, honours
`Retry-After` up to the crawl-safe delay cap, shares 429 cooldowns with sibling
requests, rewinds request bodies before replaying them, and cancels cleanly. The
original `Retry-After` value remains in debug logs so unusually long server
blocks are diagnosable. A source that panics is reported as a failed source
rather than taking down the crawl.

`total` fails closed: by default it exits non-zero if the crawl did not finish
inside its time budget. `--allow-partial` opts out of that, and prints a fourth
`partial` field on the data row instead. The Track Jobs workflow uses it, so a
deadline snapshot *is* recorded in `jobs_record.txt` — but never as an equivalent
measurement. It carries `partial` in the row, the workflow rejects it unless the
posting and source counts hold up against the previous recorded day, and the
chart draws it as an isolated diamond instead of joining the completed-crawl
trend line. See [docs/jobs-record.md](docs/jobs-record.md).

Tests are hermetic: adapter behaviour is checked against fixture responses
served through a stub HTTP transport, so the suite needs no network and runs in
under a second.

The tests that query live job boards are opt-in, because they fail whenever a
company stops using its ATS; a fact about the world rather than a regression:

```console
$ JHT_NETWORK_TESTS=1 go test ./internal/services/ -run TestGreenhouse
```

Prefer `job-hunter-toolkit health` for checking source freshness.

## License

[MIT](LICENSE)
