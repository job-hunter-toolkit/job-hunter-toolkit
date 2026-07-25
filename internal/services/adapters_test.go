package services

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// fixtureTransport serves canned responses instead of making real requests.
//
// The adapters build their own URLs internally, so rather than rewiring them for
// testability we substitute the *http.Client they already accept. That keeps
// these tests hermetic without adding injection seams to production code.
type fixtureTransport struct {
	// routes maps a substring of the request URL to the response body to serve.
	routes map[string]string

	// status is served for every matched route, defaulting to 200.
	status int

	// requests records the URLs that were requested, in order.
	requests []string
}

func (f *fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req.URL.String())

	status := f.status
	if status == 0 {
		status = http.StatusOK
	}

	for pattern, body := range f.routes {
		if strings.Contains(req.URL.String(), pattern) {
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     http.StatusText(http.StatusNotFound),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"error":"no fixture"}`)),
		Request:    req,
	}, nil
}

// fixtureClient returns a client serving the given URL-substring to body routes.
func fixtureClient(routes map[string]string) (*http.Client, *fixtureTransport) {
	transport := &fixtureTransport{routes: routes}

	return &http.Client{Transport: transport}, transport
}

// drain collects an adapter's output into postings and errors.
func drain(jobs internal.Jobs) (postings []*internal.JobPosting, errs []error) {
	for job, err := range jobs {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		postings = append(postings, job)
	}

	return postings, errs
}

func TestGreenhouseParsesPostings(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"boards-api.greenhouse.io": `{
			"jobs": [
				{
					"absolute_url": "http://boards.greenhouse.io/acme/jobs/1",
					"title": "  Security Engineer  ",
					"location": {"name": "  Remote - US  "}
				},
				{
					"absolute_url": "https://boards.greenhouse.io/acme/jobs/2",
					"title": "Application Security Engineer",
					"location": {"name": ""}
				}
			]
		}`,
	})

	postings, errs := drain(Greenhouse(t.Context(), client, "acme"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	// http:// is rewritten to https://, and surrounding whitespace is trimmed.
	if got, want := postings[0].URL, "https://boards.greenhouse.io/acme/jobs/1"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}

	if got, want := postings[0].Title, "Security Engineer"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}

	if got, want := postings[0].Location, "Remote - US"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	if got, want := postings[0].Company, "acme"; got != want {
		t.Errorf("Company = %q, want %q", got, want)
	}

	// An empty location becomes a placeholder rather than an empty string.
	if postings[1].Location == "" {
		t.Error("Location is empty, want a placeholder for missing locations")
	}
}

func TestGreenhouseReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"boards-api.greenhouse.io": `{}`},
		status: http.StatusNotFound,
	}
	client := &http.Client{Transport: transport}

	postings, errs := drain(Greenhouse(t.Context(), client, "gone"))

	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}

	// The company must be named in the error, or a crawl over a thousand
	// companies produces unattributable failures.
	if !strings.Contains(errs[0].Error(), "gone") {
		t.Errorf("error = %v, want it to name the company", errs[0])
	}
}

func TestGreenhouseReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"boards-api.greenhouse.io": `{"jobs": [ this is not json`,
	})

	postings, errs := drain(Greenhouse(t.Context(), client, "acme"))

	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
}

func TestLeverParsesPostingsAndStopsPaging(t *testing.T) {
	t.Parallel()

	// A short page tells the adapter it has reached the end, so exactly one
	// request should be made.
	client, transport := fixtureClient(map[string]string{
		"api.lever.co": `[
			{
				"text": "Staff Security Engineer",
				"hostedUrl": "https://jobs.lever.co/acme/1",
				"categories": {"location": "Remote"}
			},
			{
				"text": "Detection Engineer",
				"hostedUrl": "https://jobs.lever.co/acme/2",
				"categories": {"location": ""}
			}
		]`,
	})

	postings, errs := drain(Lever(t.Context(), client, "acme"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	if got, want := postings[0].Title, "Staff Security Engineer"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}

	if postings[1].Location == "" {
		t.Error("Location is empty, want a placeholder for missing locations")
	}

	if len(transport.requests) != 1 {
		t.Errorf("made %d requests, want 1 (a short page ends pagination)", len(transport.requests))
	}
}

func TestLeverReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"api.lever.co": `[]`},
		status: http.StatusTooManyRequests,
	}
	client := &http.Client{Transport: transport}

	_, errs := drain(Lever(t.Context(), client, "acme"))

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}

	if !strings.Contains(errs[0].Error(), "acme") {
		t.Errorf("error = %v, want it to name the company", errs[0])
	}
}

// TestJibeToleratesPolymorphicMetaData is a regression test.
//
// Jibe's top-level "meta_data" field is an object for some tenants and a bare
// `false` for others. It used to be decoded into a fixed struct, so every tenant
// sending `false` failed with a decode error, which silently disabled nine
// large employers, including the single biggest source in the project.
func TestJibeToleratesPolymorphicMetaData(t *testing.T) {
	t.Parallel()

	for _, metaData := range []string{
		`false`,
		`{"ResponseMetadata": {"requestId": "abc123"}}`,
		`null`,
	} {
		t.Run(metaData, func(t *testing.T) {
			t.Parallel()

			client, _ := fixtureClient(map[string]string{
				"jibeapply.com": `{
					"jobs": [
						{"data": {
							"title": "Cloud Security Engineer",
							"apply_url": "https://acme.jibeapply.com/jobs/1",
							"full_location": "Chicago, IL"
						}}
					],
					"totalCount": 1,
					"meta_data": ` + metaData + `
				}`,
			})

			postings, errs := drain(Jibe(t.Context(), client, "acme"))

			if len(errs) != 0 {
				t.Fatalf("errors = %v, want none regardless of meta_data shape", errs)
			}

			if len(postings) != 1 {
				t.Fatalf("got %d postings, want 1", len(postings))
			}

			if got, want := postings[0].Title, "Cloud Security Engineer"; got != want {
				t.Errorf("Title = %q, want %q", got, want)
			}

			if got, want := postings[0].Location, "Chicago, IL"; got != want {
				t.Errorf("Location = %q, want %q", got, want)
			}
		})
	}
}

func TestJibeSkipsIncompletePostings(t *testing.T) {
	t.Parallel()

	// A posting with no apply URL is not actionable, so it is dropped rather
	// than emitted with an empty link.
	client, _ := fixtureClient(map[string]string{
		"jibeapply.com": `{
			"jobs": [
				{"data": {"title": "No URL", "apply_url": "", "full_location": "Remote"}},
				{"data": {"title": "", "apply_url": "https://acme.jibeapply.com/2", "full_location": "Remote"}},
				{"data": {"title": "Good", "apply_url": "https://acme.jibeapply.com/3", "full_location": "Remote"}}
			],
			"totalCount": 3,
			"meta_data": false
		}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1 (incomplete ones are skipped)", len(postings))
	}

	if got, want := postings[0].Title, "Good"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
}

func TestAdaptersNameTheCompanyOnFailure(t *testing.T) {
	t.Parallel()

	// Every adapter must attribute failures to a company. Without this, the
	// health of ~1200 sources cannot be reported, which is how large-scale rot
	// went unnoticed in this project before.
	adapters := map[string]companyJobsFunc{
		"greenhouse": Greenhouse,
		"lever":      Lever,
		"jibe":       Jibe,
	}

	for name, adapter := range adapters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// No fixture matches, so the transport serves 404.
			client, _ := fixtureClient(map[string]string{"never-matches": `{}`})

			_, errs := drain(adapter(t.Context(), client, "sentinel-company"))

			if len(errs) == 0 {
				t.Fatal("got no errors, want one")
			}

			if !strings.Contains(errs[0].Error(), "sentinel-company") {
				t.Errorf("error = %v, want it to name the company", errs[0])
			}
		})
	}
}

func TestBuiltinRegistryIsPopulated(t *testing.T) {
	t.Parallel()

	// Each entry is one company, so the registry should be large. This guards
	// against a service file losing its init registration, which is exactly
	// how PeopleForce silently stopped being crawled.
	if len(Builtin) < 100 {
		t.Errorf("len(Builtin) = %d, want a substantial number of registered sources", len(Builtin))
	}
}

func TestWorkdayCompanyName(t *testing.T) {
	t.Parallel()

	// Regression test: this was written with strings.TrimLeft and a "https://"
	// cutset, which strips any leading run of those characters. That mangled
	// every tenant whose name began with h, t, p, or s, "pfizer" became
	// "fizer", "salesforce" became "alesforce".
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://pfizer.wd1.myworkdayjobs.com/PfizerCareers", want: "pfizer"},
		{in: "https://salesforce.wd12.myworkdayjobs.com/External_Career_Site", want: "salesforce"},
		{in: "https://tableau.wd1.myworkdayjobs.com/tableau", want: "tableau"},
		{in: "https://snapchat.wd1.myworkdayjobs.com/snap", want: "snapchat"},
		{in: "https://spe.wd1.myworkdayjobs.com/SPE", want: "spe"},
		{in: "https://hp.wd5.myworkdayjobs.com/ExternalCareerSite", want: "hp"},
		{in: "https://crowdstrike.wd5.myworkdayjobs.com/crowdstrikecareers", want: "crowdstrike"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := workdayCompanyName(tt.in); got != tt.want {
				t.Errorf("workdayCompanyName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWorkdayCXSURL(t *testing.T) {
	t.Parallel()

	// Regression test: the site path was previously interpolated as-is with
	// "/jobs" appended unconditionally. A tenant URL that already ended in
	// "/jobs", which is what you get by copying it out of a browser, produced
	// ".../jobs/jobs", which Workday answers with 404 or 405. Two real entries
	// (AES and CAE) were silently dead because of it.
	tests := []struct {
		name     string
		host     string
		company  string
		sitePath string
		want     string
	}{
		{
			name:     "plain site path",
			host:     "aes.wd1.myworkdayjobs.com",
			company:  "aes",
			sitePath: "/AES_US",
			want:     "https://aes.wd1.myworkdayjobs.com/wday/cxs/aes/AES_US/jobs",
		},
		{
			name:     "site path with trailing jobs",
			host:     "aes.wd1.myworkdayjobs.com",
			company:  "aes",
			sitePath: "/AES_US/jobs",
			want:     "https://aes.wd1.myworkdayjobs.com/wday/cxs/aes/AES_US/jobs",
		},
		{
			// Some tenants really do use "jobs" as their site id, so a
			// single-segment path must be left alone. Verified live.
			name:     "site path that is literally jobs",
			host:     "chaptershealth.wd5.myworkdayjobs.com",
			company:  "chaptershealth",
			sitePath: "/jobs",
			want:     "https://chaptershealth.wd5.myworkdayjobs.com/wday/cxs/chaptershealth/jobs/jobs",
		},
		{
			name:     "site path with trailing slash",
			host:     "cigna.wd5.myworkdayjobs.com",
			company:  "cigna",
			sitePath: "/cignacareers/",
			want:     "https://cigna.wd5.myworkdayjobs.com/wday/cxs/cigna/cignacareers/jobs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := workdayCXSURL(tt.host, tt.company, tt.sitePath); got != tt.want {
				t.Errorf("workdayCXSURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkdayCompanyURLsHaveNoRedundantJobsSuffix(t *testing.T) {
	t.Parallel()

	// Guard the convention rather than relying on people remembering it.
	//
	// Only a *redundant* suffix is wrong. Some tenants legitimately have "jobs"
	// as their site id, chaptershealth is one, verified live; so a
	// single-segment "/jobs" path is correct and must not be flagged.
	for _, raw := range WorkdayCompanyURLs {
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("tenant URL %q does not parse: %v", raw, err)

			continue
		}

		segments := strings.Split(strings.Trim(u.Path, "/"), "/")

		if len(segments) > 1 && segments[len(segments)-1] == "jobs" {
			t.Errorf("tenant URL %q has a redundant /jobs suffix; the adapter appends that itself", raw)
		}
	}
}

func TestJibeParsesCompensation(t *testing.T) {
	t.Parallel()

	// Jibe publishes pay as structured numbers with a frequency enum, and
	// populates them often, measured at 69 of 100 PetSmart postings, which makes
	// it the best pay-data source in the project.
	client, _ := fixtureClient(map[string]string{
		"jibeapply.com": `{
			"jobs": [
				{"data": {
					"title": "Pet Groomer",
					"apply_url": "https://acme.jibeapply.com/jobs/1",
					"full_location": "Signal Hill, California",
					"salary_min_value": 17.17,
					"salary_max_value": 29.95,
					"salary_currency": "",
					"salary_frequency": "HOURLY"
				}},
				{"data": {
					"title": "Undisclosed Role",
					"apply_url": "https://acme.jibeapply.com/jobs/2",
					"full_location": "Phoenix, Arizona",
					"salary_min_value": 0,
					"salary_max_value": 0,
					"salary_frequency": ""
				}}
			],
			"totalCount": 2,
			"meta_data": false
		}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	comp := postings[0].Compensation
	if comp == nil {
		t.Fatal("Compensation is nil, want a parsed hourly range")
	}

	if comp.Min != 17.17 || comp.Max != 29.95 {
		t.Errorf("range = %v-%v, want 17.17-29.95", comp.Min, comp.Max)
	}

	if comp.Period != internal.PeriodHour {
		t.Errorf("Period = %q, want %q", comp.Period, internal.PeriodHour)
	}

	// Jibe sends explicit zeros rather than omitting the fields. A zero range
	// means "not disclosed" and must not become a posting claiming to pay nothing.
	if postings[1].Compensation != nil {
		t.Errorf("Compensation = %+v, want nil for an undisclosed range", postings[1].Compensation)
	}
}

func TestAshbyPeriod(t *testing.T) {
	t.Parallel()

	// Ashby quantifies its intervals, "1 YEAR", not "YEAR"; so a bare enum
	// lookup silently produced no period at all.
	tests := map[string]internal.Period{
		"1 YEAR":  internal.PeriodYear,
		"1 MONTH": internal.PeriodMonth,
		"2 WEEKS": internal.PeriodWeek,
		"1 HOUR":  internal.PeriodHour,
		"NONE":    internal.PeriodUnknown,
		"":        internal.PeriodUnknown,
	}

	for interval, want := range tests {
		t.Run(interval, func(t *testing.T) {
			t.Parallel()

			if got := ashbyPeriod(interval); got != want {
				t.Errorf("ashbyPeriod(%q) = %q, want %q", interval, got, want)
			}
		})
	}
}

func TestAshbyParsesCompensationAndRemote(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"api.ashbyhq.com": `{
			"jobs": [
				{
					"jobUrl": "https://jobs.ashbyhq.com/acme/1",
					"title": "Staff Engineer",
					"location": "San Francisco",
					"isRemote": true,
					"compensation": {
						"compensationTierSummary": "$236K – $290K • Offers Equity",
						"compensationTiers": [
							{"components": [
								{"compensationType": "EquityPercentage", "interval": "NONE",
								 "currencyCode": null, "minValue": null, "maxValue": null},
								{"compensationType": "Salary", "interval": "1 YEAR",
								 "currencyCode": "USD", "minValue": 236000, "maxValue": 290000}
							]}
						]
					}
				},
				{
					"jobUrl": "https://jobs.ashbyhq.com/acme/2",
					"title": "No Pay Published",
					"location": "New York",
					"isRemote": false,
					"compensation": {"compensationTierSummary": "", "compensationTiers": []}
				}
			]
		}`,
	})

	postings, errs := drain(AshbyHQ(t.Context(), client, "acme"))
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	// Without ?includeCompensation=true the compensation key is absent entirely,
	// so the request must ask for it.
	if len(transport.requests) == 0 || !strings.Contains(transport.requests[0], "includeCompensation=true") {
		t.Errorf("request = %v, want it to ask for compensation", transport.requests)
	}

	comp := postings[0].Compensation
	if comp == nil {
		t.Fatal("Compensation is nil, want the salary component parsed")
	}

	// Only the Salary component is a comparable range; equity must not overwrite
	// it with nulls.
	if comp.Min != 236000 || comp.Max != 290000 {
		t.Errorf("range = %v-%v, want 236000-290000", comp.Min, comp.Max)
	}

	if comp.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", comp.Currency)
	}

	if comp.Period != internal.PeriodYear {
		t.Errorf("Period = %q, want %q", comp.Period, internal.PeriodYear)
	}

	if comp.Summary == "" {
		t.Error("Summary is empty, want the board's own rendering kept")
	}

	if postings[0].Remote == nil || !*postings[0].Remote {
		t.Error("Remote flag was not carried through for a remote posting")
	}

	if postings[1].Remote == nil || *postings[1].Remote {
		t.Error("Remote flag was not carried through for an onsite posting")
	}

	if postings[1].Compensation != nil {
		t.Errorf("Compensation = %+v, want nil when nothing is published", postings[1].Compensation)
	}
}

func TestLeverParsesSalaryRange(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"api.lever.co": `[
			{
				"text": "Security Engineer",
				"hostedUrl": "https://jobs.lever.co/acme/1",
				"categories": {"location": "Remote"},
				"salaryRange": {"min": 150000, "max": 200000, "currency": "USD", "interval": "per-year-salary"}
			},
			{
				"text": "No Range",
				"hostedUrl": "https://jobs.lever.co/acme/2",
				"categories": {"location": "Remote"}
			}
		]`,
	})

	postings, errs := drain(Lever(t.Context(), client, "acme"))
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	comp := postings[0].Compensation
	if comp == nil {
		t.Fatal("Compensation is nil, want the salaryRange parsed")
	}

	if comp.Min != 150000 || comp.Max != 200000 || comp.Currency != "USD" {
		t.Errorf("got %+v, want 150000-200000 USD", comp)
	}

	// Lever spells the interval as a slug, so the unit is matched inside it.
	if comp.Period != internal.PeriodYear {
		t.Errorf("Period = %q, want %q", comp.Period, internal.PeriodYear)
	}

	if postings[1].Compensation != nil {
		t.Errorf("Compensation = %+v, want nil when salaryRange is absent", postings[1].Compensation)
	}
}

func TestSmartRecruitersSetsCompanyAndPaginates(t *testing.T) {
	t.Parallel()

	// Regression test. The previous implementation had four defects at once:
	// first-page postings were yielded with no Company set at all, the first page
	// used http.DefaultClient instead of the client passed in (so no retries, no
	// User-Agent, no per-host limit), the pagination error was discarded, and the
	// offset advanced by len(content) so an empty page looped forever.
	client, transport := fixtureClient(map[string]string{
		"offset=0": `{"offset":0,"limit":2,"totalFound":3,"content":[
			{"id":"1","name":"Security Analyst","location":{"city":"Austin","region":"TX","country":"us"}},
			{"id":"2","name":"Store Manager","location":{"city":"Dallas","region":"TX","country":"us"}}
		]}`,
		"offset=2": `{"offset":2,"limit":2,"totalFound":3,"content":[
			{"id":"3","name":"Pharmacist","location":{"city":"Houston","region":"TX","country":"us"}}
		]}`,
	})

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 3 {
		t.Fatalf("got %d postings, want 3 across two pages", len(postings))
	}

	// Every posting must name its company, including those from the first page.
	for i, p := range postings {
		if p.Company != "acme" {
			t.Errorf("posting %d Company = %q, want %q", i, p.Company, "acme")
		}

		if p.URL == "" || p.Title == "" {
			t.Errorf("posting %d is incomplete: %+v", i, p)
		}
	}

	if len(transport.requests) != 2 {
		t.Errorf("made %d requests, want 2", len(transport.requests))
	}
}

func TestSmartRecruitersStopsOnEmptyPage(t *testing.T) {
	t.Parallel()

	// totalFound claims more postings than the API actually returns. The adapter
	// must stop rather than request the same empty page forever.
	client, transport := fixtureClient(map[string]string{
		"offset=0": `{"offset":0,"limit":2,"totalFound":500,"content":[
			{"id":"1","name":"Only Posting","location":{"city":"Austin","region":"TX","country":"us"}}
		]}`,
		"offset=1": `{"offset":1,"limit":2,"totalFound":500,"content":[]}`,
	})

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 1 {
		t.Errorf("got %d postings, want 1", len(postings))
	}

	if len(transport.requests) > 3 {
		t.Errorf("made %d requests, want it to stop at the empty page", len(transport.requests))
	}
}

func TestSmartRecruitersReportsPaginationError(t *testing.T) {
	t.Parallel()

	// A failure on page two used to be discarded silently.
	transport := &fixtureTransport{
		routes: map[string]string{"offset=0": `{"totalFound":10,"content":[]}`},
		status: http.StatusInternalServerError,
	}

	_, errs := drain(SmartRecruiters(t.Context(), &http.Client{Transport: transport}, "acme"))

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}

	if !strings.Contains(errs[0].Error(), "acme") {
		t.Errorf("error = %v, want it to name the company", errs[0])
	}
}
