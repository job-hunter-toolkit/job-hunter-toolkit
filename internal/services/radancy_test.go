package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/net/html"
)

// radancyEnvelope wraps a results fragment the way the live endpoint does, so a
// fixture can be written as readable HTML rather than as a JSON string literal
// with every quote escaped.
func radancyEnvelope(t *testing.T, fragment string) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"filters":    "",
		"results":    fragment,
		"hasJobs":    true,
		"hasContent": false,
	})
	must.NoError(t, err)

	return string(body)
}

// radancyResultsSection wraps rows in the section the live endpoint always
// emits, including the count attribute this adapter treats as proof that the
// response really is a Radancy results fragment.
func radancyResultsSection(total int, rows string) string {
	return fmt.Sprintf(`<section id="search-results" data-total-results="%d" data-total-job-results="%d">
		<ul>%s</ul>
	</section>`, total, total, rows)
}

// radancyFixture reads a fragment captured from a live Radancy career site.
//
// The captures under testdata are byte-for-byte what the endpoint returned on
// 2026-07-28 for RecordsPerPage=6, with nothing removed: the "filters" key is
// empty at that page size on every tenant, so the whole response is already
// small.
func radancyFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

func TestRadancyParsesPostings(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"www.att.jobs": radancyEnvelope(t, radancyResultsSection(2, `
			<li>
				<div class="section8__job-data">
					<h2 class="headline__small"><a href="/job/chico/retail-sales-consultant/117/98395039840" data-job-id="98395039840">  Retail Sales Consultant  </a></h2>
					<span class="job-location">  Chico, California  </span>
				</div>
				<button type="button" class="js-save-job-btn" data-job-id="98395039840"><span class="visually-hidden">Save Role</span></button>
			</li>
			<li>
				<div class="section8__job-data">
					<h2 class="headline__small"><a href="/job/houston/integrated-sales-support-rep/117/98395039792" data-job-id="98395039792">Integrated Sales Support Rep</a></h2>
				</div>
			</li>`)),
	})

	postings, errs := drain(Radancy(t.Context(), client, "att,www.att.jobs"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	first := postings[0]

	test.Eq(t, "att", first.Company)
	test.Eq(t, "https://www.att.jobs/job/chico/retail-sales-consultant/117/98395039840", first.URL)
	test.Eq(t, "Retail Sales Consultant", first.Title)
	test.Eq(t, "Chico, California", first.Location)
	test.Eq(t, "98395039840", first.ExternalID)
	test.Eq(t, radancyPlatform, first.Source.Platform)
	test.Eq(t, "att,www.att.jobs", first.Source.Key)

	// A board that publishes no location for a posting says so by omission; the
	// adapter must not invent one, and must not drop the posting either.
	test.Eq(t, "unknown/remote", postings[1].Location)

	must.Len(t, 1, transport.requests)
	test.StrContains(t, transport.requests[0], "https://www.att.jobs/search-jobs/results?")
	test.StrContains(t, transport.requests[0], "CurrentPage=1")
	test.StrContains(t, transport.requests[0], "RecordsPerPage=1000")

	// The one parameter without which the endpoint answers HTTP 200 with an
	// empty results string, making every live tenant look like an empty board.
	test.StrContains(t, transport.requests[0], "SearchResultsModuleName=Search+Results")
}

// TestRadancyBuildsTheLocalePrefixedEndpoint covers the half of the tenant key
// that cannot be derived. Six of the seventeen registered tenants serve their
// board only under a locale segment, and jobs.veolia.com answers HTTP 301 for
// the unprefixed path, so dropping the prefix does not degrade a tenant, it
// removes it.
func TestRadancyBuildsTheLocalePrefixedEndpoint(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"jobs.veolia.com": radancyEnvelope(t, radancyResultsSection(0, "")),
	})

	_, errs := drain(Radancy(t.Context(), client, "veolia,jobs.veolia.com,/fr"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, transport.requests)
	test.StrContains(t, transport.requests[0], "https://jobs.veolia.com/fr/search-jobs/results?")
}

func TestRadancyRejectsMalformedTenantKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "att", "a,b,c,d", ",www.att.jobs", "att,"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			client, transport := fixtureClient(map[string]string{"never-matches": `{}`})

			postings, errs := drain(Radancy(t.Context(), client, key))

			must.SliceEmpty(t, postings)
			must.Len(t, 1, errs)
			test.StrContains(t, errs[0].Error(), "malformed Radancy tenant")

			// A key that cannot be parsed must not reach the network at all.
			must.SliceEmpty(t, transport.requests)
		})
	}
}

func TestRadancyReportsNon200(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{"www.att.jobs": `{}`})
	transport.status = http.StatusServiceUnavailable

	postings, errs := drain(Radancy(t.Context(), client, "att,www.att.jobs"))

	must.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "unexpected status code")
	test.StrContains(t, errs[0].Error(), `"att"`)
}

func TestRadancyReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"www.att.jobs": `{"results": `})

	postings, errs := drain(Radancy(t.Context(), client, "att,www.att.jobs"))

	must.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "failed to decode response")
	test.StrContains(t, errs[0].Error(), `"att"`)
}

// TestRadancyDistinguishesAnEmptyBoardFromAWrongHost is the check that keeps a
// misfiled host from sitting in the registry forever reporting nothing.
//
// Both cases are HTTP 200 with no postings, and nothing else in the response
// separates them. A real Radancy search that matches no posting still renders
// its results section with the count set to zero — measured on www.att.jobs
// with a nonsense keyword — so the count attribute's presence is the platform
// test and its absence is an error worth reporting.
func TestRadancyDistinguishesAnEmptyBoardFromAWrongHost(t *testing.T) {
	t.Parallel()

	t.Run("empty board", func(t *testing.T) {
		t.Parallel()

		client, _ := fixtureClient(map[string]string{
			"www.att.jobs": radancyEnvelope(t, radancyResultsSection(0, "")),
		})

		postings, errs := drain(Radancy(t.Context(), client, "att,www.att.jobs"))

		must.SliceEmpty(t, errs)
		must.SliceEmpty(t, postings)
	})

	t.Run("not a Radancy board", func(t *testing.T) {
		t.Parallel()

		client, _ := fixtureClient(map[string]string{
			"www.att.jobs": radancyEnvelope(t, `<div class="something-else"><p>No results</p></div>`),
		})

		postings, errs := drain(Radancy(t.Context(), client, "att,www.att.jobs"))

		must.SliceEmpty(t, postings)
		must.Len(t, 1, errs)
		test.StrContains(t, errs[0].Error(), "carries no result count")
	})
}

// radancyPagedTransport serves a distinct page per CurrentPage value.
//
// The shared fixtureTransport matches on a URL substring and every page shares
// "search-jobs/results", so a paginated fixture built on it would serve whichever
// route the map iterated to first.
type radancyPagedTransport struct {
	// pages maps a CurrentPage value to the body to serve. A page not present
	// is served as an empty board.
	pages map[string]string

	// fixed, when non-empty, is served for every page regardless of
	// CurrentPage: a board that ignores its page parameter.
	fixed string

	requests []string
}

func (r *radancyPagedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, req.URL.String())

	body := r.fixed
	if body == "" {
		page := req.URL.Query().Get("CurrentPage")

		var ok bool
		if body, ok = r.pages[page]; !ok {
			body = `{"results": "<section data-total-job-results=\"0\"></section>"}`
		}
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// radancyRows renders count rows whose ids start at start, enough of a row to
// exercise the parser and nothing more.
func radancyRows(start, count int) string {
	var out strings.Builder

	for i := range count {
		id := start + i

		fmt.Fprintf(&out, `<li><a href="/job/x/role-%d/1/%d" data-job-id="%d"><h2>Role %d</h2><span class="job-location">Springfield, IL</span></a></li>`,
			id, id, id, id)
	}

	return out.String()
}

// TestRadancyPaginatesUntilTheBoardRunsOut walks a tenant larger than one page
// and stops on the short page rather than asking for one more.
func TestRadancyPaginatesUntilTheBoardRunsOut(t *testing.T) {
	t.Parallel()

	transport := &radancyPagedTransport{pages: map[string]string{
		"1": radancyEnvelope(t, radancyResultsSection(1500, radancyRows(1, radancyPageSize))),
		"2": radancyEnvelope(t, radancyResultsSection(1500, radancyRows(radancyPageSize+1, 500))),
	}}

	postings, errs := drain(Radancy(t.Context(), &http.Client{Transport: transport}, "att,www.att.jobs"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1500, postings)
	must.Len(t, 2, transport.requests)

	test.StrContains(t, transport.requests[0], "CurrentPage=1")
	test.StrContains(t, transport.requests[1], "CurrentPage=2")
}

// TestRadancyStopsWhenTheBoardStatesItIsFinished covers the other exit: a
// tenant whose page is full but whose stated total has been reached. Radancy
// serves a page past the end as HTTP 200 with the real total and zero rows, so
// without this the adapter would spend one request per tenant learning that.
func TestRadancyStopsWhenTheBoardStatesItIsFinished(t *testing.T) {
	t.Parallel()

	transport := &radancyPagedTransport{pages: map[string]string{
		"1": radancyEnvelope(t, radancyResultsSection(radancyPageSize, radancyRows(1, radancyPageSize))),
	}}

	postings, errs := drain(Radancy(t.Context(), &http.Client{Transport: transport}, "att,www.att.jobs"))

	must.SliceEmpty(t, errs)
	must.Len(t, radancyPageSize, postings)
	must.Len(t, 1, transport.requests)
}

// TestRadancyStopsOnABoardThatIgnoresItsPageParameter is a regression test for
// the failure this codebase has already paid for twice: an adapter that ends
// only on a short page, pointed at a board that answers every page with the
// same first page, issues requests until the crawl deadline and yields
// duplicates the whole time. Replayed against exactly such a stub, Lever, Jibe
// and Phenom each produced 500,001 postings in under a second.
//
// The guard must fire before anything from the repeated page is yielded, so
// this asserts both the request count and the posting count.
func TestRadancyStopsOnABoardThatIgnoresItsPageParameter(t *testing.T) {
	t.Parallel()

	transport := &radancyPagedTransport{
		fixed: radancyEnvelope(t, radancyResultsSection(50000, radancyRows(1, radancyPageSize))),
	}

	postings, errs := drain(Radancy(t.Context(), &http.Client{Transport: transport}, "att,www.att.jobs"))

	must.SliceEmpty(t, errs)
	must.Len(t, radancyPageSize, postings)
	must.Len(t, 2, transport.requests)
}

// TestRadancyRefusesToPaginateForever is the backstop for a board that varies
// its pages without ever running out, which [pageRepeatGuard] cannot see. The
// live result window ends a real tenant at 10,000 rows; this is what happens if
// one ever does not.
func TestRadancyRefusesToPaginateForever(t *testing.T) {
	t.Parallel()

	pages := map[string]string{}
	for page := 1; page <= 100; page++ {
		pages[fmt.Sprint(page)] = radancyEnvelope(t,
			radancyResultsSection(1_000_000, radancyRows((page-1)*radancyPageSize+1, radancyPageSize)))
	}

	transport := &radancyPagedTransport{pages: pages}

	postings, errs := drain(Radancy(t.Context(), &http.Client{Transport: transport}, "att,www.att.jobs"))

	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "refusing to keep paginating")

	// Bounded by the result window rather than by radancyMaxPages, which is the
	// looser of the two.
	bound := radancyMaxWindow / radancyPageSize

	must.Len(t, bound, transport.requests)
	must.Len(t, bound*radancyPageSize, postings)
}

// TestRadancyReadsEverySkinItWasMeasuredAgainst is the test that decides whether
// this adapter reads Radancy, as opposed to reading the shape a document said
// Radancy has.
//
// docs/research/ats-platform-survey.md states the skin-agnostic invariant as an
// <a> carrying both data-job-id and `href="/job/..."`, with optional per-row
// classes job-location, job-date-posted and job-brand. Probed live on
// 2026-07-28 across fourteen tenants, the anchor half of that holds and the rest
// does not, and each capture below is one of the ways it does not:
//
//   - att: the location is not inside the anchor. It is a sibling of the
//     anchor's grandparent, so anything that reads only the anchor's subtree
//     loses the location on this tenant.
//   - disney: the row is a <tr> and not an <li>, with the title, date, brand and
//     location in four separate <td>s; and the date is a month name, written
//     "Jul. 27, 2026" with a trailing period except for May, which needs a
//     second layout.
//   - veolia: the href is "/fr/emploi/...". The path segment is translated, so
//     requiring "/job/" finds none of this tenant's 2,962 postings. It also
//     renders two anchors per posting with the same data-job-id, so without
//     deduplication this tenant returns exactly twice its board.
//   - vinci: there is no heading element at all — the title is a class — and
//     every location and category is repeated in an sr-only span, so reading the
//     anchor's text yields the title, location and category run together, twice.
//   - tenethealth: the field's own caption is inside the field
//     (<span class="heading">Job Type: </span>PRN).
//   - aldi: there is no job-location at all, only a job-address-list holding a
//     full street address, and its captions are <b> rather than a class.
func TestRadancyReadsEverySkinItWasMeasuredAgainst(t *testing.T) {
	t.Parallel()

	type want struct {
		fixture    string
		key        string
		postings   int
		url        string
		title      string
		location   string
		department string
		employment string
		posted     time.Time
	}

	for _, tc := range []want{
		{
			fixture:  "radancy_att_results.json",
			key:      "att,www.att.jobs",
			postings: 6,
			url:      "https://www.att.jobs/job/chico/retail-sales-consultant/117/98395039840",
			title:    "Retail Sales Consultant",
			location: "Chico, California",
		},
		{
			fixture:  "radancy_disney_results.json",
			key:      "disney,jobs.disneycareers.com",
			postings: 6,
			url:      "https://jobs.disneycareers.com/job/burbank/sr-executive-assistant-ph/391/96053617056",
			title:    "Sr Executive Assistant (PH)",
			location: "Burbank, California, United States",
			posted:   time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC),
		},
		{
			fixture:  "radancy_veolia_results.json",
			key:      "veolia,jobs.veolia.com,/fr",
			postings: 6,
			url:      "https://jobs.veolia.com/fr/emploi/sillery/electromecanicien-f-h/3091/19904869696",
			title:    "Electromécanicien F/H",
			location: "Sillery, France",
		},
		{
			fixture:    "radancy_vinci_results.json",
			key:        "vinci,jobs.vinci.com,/en",
			postings:   6,
			url:        "https://jobs.vinci.com/en/job/frankfurt-am-main/monteur-in-w-m-d-elektrotechnik-automatisierungstechnik-emsr-msr-industrieanlagen/1440/2266761408",
			title:      "Monteur:in (w/m/d) Elektrotechnik / Automatisierungstechnik / EMSR / MSR / Industrieanlagen",
			location:   "Frankfurt am Main, Hessen",
			department: "OPERATIONS / MAINTENANCE",
		},
		{
			fixture:    "radancy_tenethealth_results.json",
			key:        "tenethealth,jobs.tenethealth.com",
			postings:   6,
			url:        "https://jobs.tenethealth.com/job/town-of-framingham/certified-surgical-tech/1127/98376917440",
			title:      "Certified Surgical tech",
			location:   "Town of Framingham, MA",
			employment: "",
		},
		{
			fixture:    "radancy_aldi_results.json",
			key:        "aldi,careers.aldi.us",
			postings:   6,
			url:        "https://careers.aldi.us/job/north-richland-hills/full-time-assistant-store-manager/61/98393877456",
			title:      "Full-Time Assistant Store Manager",
			location:   "8537 Davis Blvd, North Richland Hills, TX, USA, 76182",
			department: "Store",
			employment: "full_time",
		},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			tenant, err := parseRadancyTenant(tc.key)
			must.NoError(t, err)

			client, _ := fixtureClient(map[string]string{
				tenant.host: radancyFixture(t, tc.fixture),
			})

			postings, errs := drain(Radancy(t.Context(), client, tc.key))

			must.SliceEmpty(t, errs)
			must.Len(t, tc.postings, postings)

			first := postings[0]

			test.Eq(t, tc.url, first.URL)
			test.Eq(t, tc.title, first.Title)
			test.Eq(t, tc.location, first.Location)
			test.Eq(t, tc.department, first.Department)
			test.Eq(t, tc.employment, string(first.EmploymentType))
			test.Eq(t, tc.posted, first.PostedAt)

			// Whatever the skin, no posting may come out with a missing title or
			// a URL that did not resolve against the tenant's host.
			for _, posting := range postings {
				test.NotEq(t, "", posting.Title)
				test.NotEq(t, "", posting.ExternalID)
				test.StrHasPrefix(t, "https://"+tenant.host+"/", posting.URL)
			}
		})
	}
}

// TestRadancyReadsBothPublishedDateSpellings pins the one field where getting
// the format wrong is invisible downstream: a date parsed a month out lands in
// [internal.Filter.PostedSince] where nothing can notice.
//
// Reading the slash form as month-first is a measurement, not the usual
// assumption. Across 2,016 dated postings from careers.munichre.com and
// jobs.wegmans.com the second component reached 31 and the first never exceeded
// 12, and no month is the 31st, which rules day-first out.
func TestRadancyReadsBothPublishedDateSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		published string
		want      time.Time
	}{
		{"07/28/2026", time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)},
		{"06/16/2026", time.Date(2026, time.June, 16, 0, 0, 0, 0, time.UTC)},
		{"Jul. 27, 2026", time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC)},
		{"May 18, 2026", time.Date(2026, time.May, 18, 0, 0, 0, 0, time.UTC)},
		{"", time.Time{}},
		{"yesterday", time.Time{}},
	} {
		t.Run(tc.published, func(t *testing.T) {
			t.Parallel()

			rows := fmt.Sprintf(
				`<li><a href="/job/a/b/1/2" data-job-id="2"><h2>Role</h2><span class="job-date-posted">%s</span></a></li>`,
				tc.published)

			client, _ := fixtureClient(map[string]string{
				"www.att.jobs": radancyEnvelope(t, radancyResultsSection(1, rows)),
			})

			postings, errs := drain(Radancy(t.Context(), client, "att,www.att.jobs"))

			must.SliceEmpty(t, errs)
			must.Len(t, 1, postings)
			test.Eq(t, tc.want, postings[0].PostedAt)
		})
	}
}

// TestRadancyPrefersTheJobCountOverTheResultCount covers a disagreement only one
// tenant showed. jobs.wegmans.com states data-total-results 605 and
// data-total-job-results 499; the difference is 106 non-job content pages that
// the same search matched. Reading the larger number would make the adapter ask
// for a page that does not exist on every crawl of that tenant.
func TestRadancyPrefersTheJobCountOverTheResultCount(t *testing.T) {
	t.Parallel()

	fragment := fmt.Sprintf(
		`<section data-total-results="605" data-total-job-results="%d"><ul>%s</ul></section>`,
		radancyPageSize, radancyRows(1, radancyPageSize))

	transport := &radancyPagedTransport{pages: map[string]string{"1": radancyEnvelope(t, fragment)}}

	postings, errs := drain(Radancy(t.Context(), &http.Client{Transport: transport}, "wegmans,jobs.wegmans.com,/en"))

	must.SliceEmpty(t, errs)
	must.Len(t, radancyPageSize, postings)
	must.Len(t, 1, transport.requests)
}

// TestRadancyRegisteredTenantsComeFromTheCandidateFile keeps the registered list
// traceable, and checks the three invariants a comma-keyed, host-keyed registry
// needs.
//
// The third is specific to this platform and is not a theoretical worry. Radancy
// publishes one board per brand and routes locales by path prefix, so
// jobs.veolia.com/fr and jobs.veolia.com/en are the same 2,962 postings, and
// jobs.disneycareers.com, emplois.disneycareers.com and www.disneycareers.com/en
// are the same 800. Registering two of those is not extra coverage, it is one
// employer crawled twice under URLs [internal.Dedupe] cannot collapse.
func TestRadancyRegisteredTenantsComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "radancy_hosts.txt")

	must.Greater(t, 10, len(candidates), must.Sprint("the candidate file should hold the full probed list"))

	var (
		hosts   = make(map[string]string, len(RadancyTenants))
		company = make(map[string]string, len(RadancyTenants))
	)

	for _, key := range RadancyTenants {
		tenant, err := parseRadancyTenant(key)
		must.NoError(t, err, must.Sprintf("registered tenant %q does not parse", key))

		test.True(t, candidates[key],
			test.Sprintf("registered tenant %q is not in testdata/candidates/radancy_hosts.txt", key))

		previous, seen := hosts[tenant.host]
		test.False(t, seen, test.Sprintf(
			"tenant %q registers host %q, already registered as %q. Radancy routes locales by path "+
				"prefix on one board per brand, so two prefixes of one host are the same postings twice",
			key, tenant.host, previous))

		hosts[tenant.host] = key

		previous, seen = company[tenant.company]
		test.False(t, seen, test.Sprintf("tenant %q duplicates the company already registered as %q", key, previous))

		company[tenant.company] = key
	}
}

// TestRadancyTenantKeyRoundTrips keeps [internal.PostingSource.Key] equal to
// [Source.Key]. They are the join key docs/architecture-roadmap.md settles on,
// and a crawl has already had a bug where a source key and a derived name were
// compared and every posting silently discarded.
func TestRadancyTenantKeyRoundTrips(t *testing.T) {
	t.Parallel()

	for _, key := range RadancyTenants {
		tenant, err := parseRadancyTenant(key)
		must.NoError(t, err)

		test.Eq(t, key, tenantKey(tenant))
		test.Eq(t, tenant.company, radancyCompanyName(key))
	}

	// A prefix written without its leading slash, or with a trailing one, is
	// normalised for the URL but must not silently become a different key than
	// the one the registry holds.
	tenant, err := parseRadancyTenant("veolia,jobs.veolia.com,fr/")
	must.NoError(t, err)
	test.Eq(t, "/fr", tenant.prefix)
}

// TestRadancyCompanyNameFallsBackToTheKey keeps a malformed entry visible in the
// company list rather than collapsing to an empty name that sorts first and
// matches every `--company` term.
func TestRadancyCompanyNameFallsBackToTheKey(t *testing.T) {
	t.Parallel()

	test.Eq(t, "nonsense", radancyCompanyName("nonsense"))
}

// TestRadancyValueTextDropsCaptionsAndScreenReaderCopies documents the two
// classes of noise the skins put inside a field's own element, both measured.
//
// Left in, the captions become part of the value — a Takeda role's department
// would be filed as "Category: Medical Affairs" — and the screen-reader copies
// double Vinci's location and category, because that tenant repeats each of them
// in an sr-only span immediately after the visible one.
func TestRadancyValueTextDropsCaptionsAndScreenReaderCopies(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, fragment, want string }{
		{"tenet caption span", `<span><span class="heading">Job Type: </span>PRN</span>`, "PRN"},
		{"aldi bold caption", `<span><b>Position Type</b> | Full-Time</span>`, "Full-Time"},
		{"takeda strong caption", `<span><strong>Category: </strong>Medical Affairs</span>`, "Medical Affairs"},
		{"vinci sr-only copy", `<span>Hessen<span class="sr-only">Hessen</span></span>`, "Hessen"},
		{"collapsed whitespace", `<span>  Chico,   California  </span>`, "Chico, California"},
		{"nested icon span", `<span><span class="location-icon-red"></span> Sillery, France </span>`, "Sillery, France"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, err := html.Parse(strings.NewReader(tc.fragment))
			must.NoError(t, err)

			var (
				span *html.Node
				walk func(*html.Node)
			)

			walk = func(n *html.Node) {
				if span != nil {
					return
				}

				if n.Type == html.ElementNode && n.Data == "span" {
					span = n
					return
				}

				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}

			walk(doc)
			must.NotNil(t, span)

			test.Eq(t, tc.want, radancyValueText(span))
		})
	}
}
