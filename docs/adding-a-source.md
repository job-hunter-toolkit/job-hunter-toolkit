# Adding a job source

Almost always, adding a company is a one-line change: append its board slug to
the right list in `internal/services`.

```go
var GreenhouseCompanies = []string{
	// ...
	"newcompany",
}
```

## Verify before you add

**Never add a slug you have not verified against the live API.** An unverified
slug is worse than a missing one: it becomes a permanent error in every crawl,
and at this scale nobody notices one more failing source. That is exactly how
this project's coverage silently decayed by two thirds between 2020 and 2025.

Each platform has a public endpoint you can check directly:

| Platform | Verify with |
| --- | --- |
| Greenhouse | `https://boards-api.greenhouse.io/v1/boards/<slug>/jobs` |
| Ashby | `https://api.ashbyhq.com/posting-api/job-board/<slug>` |
| Lever | `https://api.lever.co/v0/postings/<slug>?mode=json` |
| SmartRecruiters | `https://api.smartrecruiters.com/v1/companies/<slug>/postings` |
| BambooHR | `https://<slug>.bamboohr.com/careers/list` |
| Workable | `https://apply.workable.com/api/v3/accounts/<slug>/jobs` |
| Rippling | `https://ats.rippling.com/<slug>/jobs` |
| Jibe / iCIMS | `https://<slug>.jibeapply.com/api/jobs?limit=100&page=1` |
| Jobvite | `https://jobs.jobvite.com/<slug>/search?nl=1&p=1` |
| PeopleForce | `https://<slug>.peopleforce.io/careers` |
| Workday | full tenant URL, e.g. `https://<tenant>.wd1.myworkdayjobs.com/<site-id>` |

```console
$ curl -sS -o /dev/null -w '%{http_code}\n' \
    -A 'job-hunter-toolkit/health-check' \
    https://boards-api.greenhouse.io/v1/boards/newcompany/jobs
200
```

HTTP 200 means the board exists. For the HTML-scraped platforms (Rippling,
Jobvite, PeopleForce) a 200 is not sufficient, those serve a landing page for
unknown tenants, so confirm the body actually lists jobs.

Or just let the tool tell you:

```console
$ go run . health --company newcompany
ok       newcompany    42 postings
```

## HTTP 200 with zero postings is not a broken source

It means the company is not hiring today. Keep those entries; they will start
producing again. Only remove a slug on a definitive 404, 410, or DNS failure,
confirmed more than once so a transient blip does not delete real coverage.

**An HTTP 429 is not a dead source either.** It usually means the crawl was too
aggressive, not that the board is gone. In one measured run, 93 of 106 apparent
failures were self-inflicted 429s.

## A 200 does not always mean the tenant exists

Two platforms need more than a status code to verify:

- **SmartRecruiters** returns `HTTP 200` with `totalFound: 0` for *any* string,
  including nonsense. Gate on `totalFound > 0` and check that the returned
  posting's company identity actually matches who you think it is.
- **Rippling**, **Jobvite**, and **PeopleForce** serve a 200 landing page for
  unknown tenants. Confirm the body really lists jobs.

Watch out for sandbox artifacts too: a board whose only posting is titled
"Test UAT" or "Automationjob_dontdelete" is not a real board.

## Beware ambiguous single-word slugs

Slugs are first-come-first-served per platform, so a short name often belongs to a
different company than the famous one. Verified examples already in this repo:

| Slug | Actually is | Not |
| --- | --- | --- |
| `warp` (Ashby) | Warp, the NYC payroll/HR fintech | Warp.dev, the terminal |
| `neon` (Ashby) | a payments company | Neon.tech, the Postgres company |
| `safe` (Ashby) | Safe Software, maker of FME | Safe/Gnosis, the crypto wallet |
| `cedar` (Ashby) | a home-affordability/mortgage startup | Cedar, the patient-billing fintech |
| `flock` (Ashby) | Flock, a UK motor-fleet insurtech | Flock Safety, the US camera company |
| `lightspeed` (Ashby) | LightSpeed Build Technologies, construction robotics | Lightspeed Commerce, the POS company |
| `otter` (Greenhouse) | Otter, restaurant operations | Otter.ai |
| `prisma` (Greenhouse) | a LatAm fintech | Prisma, the ORM |
| `axiom` (Greenhouse) | Axiom Law | Axiom, the observability company |
| `glean` (SmartRecruiters) | Glean.ai, expense management | Glean.com, enterprise search |

These are all legitimate sources; they are simply not the company the name
suggests. Check a couple of job titles before assuming an identity, and prefer a
longer, unambiguous slug when one exists.

## Adding a whole platform

Prefer this over a one-off company scraper. One platform adapter covers every
company on that platform for roughly the same effort, which is the property that
makes this project scale.

Create `internal/services/<platform>.go` following an existing adapter.
`greenhouse.go` is the simplest, `lever.go` shows pagination:

```go
func init() {
	registerBuiltin(multiJobsFunc(Platform, PlatformCompanies))
}

var PlatformCompanies = []string{}

func Platform(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		// build request with http.NewRequestWithContext
		// check resp.StatusCode
		// decode, then yield one *internal.JobPosting per posting
	}
}
```

Requirements for a new adapter:

- **Name the company in every error.** `fmt.Errorf("... for %q: %w", company, err)`.
  Without this, a failure among a thousand sources is unattributable, and the
  `health` command becomes useless.
- **Model only the fields you use.** Do not paste a generated struct for the
  whole response. Job board APIs are polymorphic across tenants: Jibe's
  top-level `meta_data` is an object for some companies and a bare `false` for
  others, and modelling it broke nine large employers at once.
- **Respect `ctx`**, check `ctx.Err()` inside loops so a cancelled crawl stops.
- **Close response bodies per request.** In a paginated loop, put the fetch in
  its own function rather than `defer`ring inside the loop, or bodies accumulate
  for the whole crawl. See `leverPage` and `jibePage`.
- **Do not build your own HTTP client.** Use the `*http.Client` you are handed;
  it already retries transient failures and sets a User-Agent.

## Testing

Add a hermetic test to `internal/services/adapters_test.go`. Adapters build
their own URLs, so tests substitute the `*http.Client` with a stub transport
serving fixture responses, no network, no injection seams in production code:

```go
client, _ := fixtureClient(map[string]string{
	"platform.example": `{"jobs": [...]}`,
})

postings, errs := drain(Platform(t.Context(), client, "acme"))
```

Cover at minimum: a normal response, a non-200 status, and malformed JSON. If
you fixed a real-world parsing quirk, add a regression case for it, see
`TestJibeToleratesPolymorphicMetaData`.

The live-network tests in `helpers_test.go` are opt-in and are a source-health
check, not a correctness gate:

```console
$ JHT_NETWORK_TESTS=1 go test ./internal/services/ -run TestGreenhouse
```

## Company-specific adapters

For employers running their own careers site, `internal/companies` holds
bespoke adapters (see `oxide.go`). These are a last resort: they break whenever
the site is redesigned. Check first whether the company is on a shared platform
that deserves its own adapter instead, `docs/source-backlog.md` tracks
candidates.

## Before opening a PR

```console
$ gofmt -l .
$ go build ./...
$ go vet ./...
$ go test ./...
```

CI additionally runs `staticcheck` and `govulncheck`.
