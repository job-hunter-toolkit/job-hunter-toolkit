package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// amazonRouteTransport serves one response per exact request URL. Every page of
// this board shares the substring "/en/search.json", so the shared
// fixtureTransport's substring matching cannot express a sliced, paginated
// walk; this is the same shape as eightfoldPageTransport.
type amazonRouteTransport struct {
	pages    map[string]string
	requests []string
}

func (tr *amazonRouteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.requests = append(tr.requests, req.URL.String())

	body, ok := tr.pages[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     http.StatusText(http.StatusNotFound),
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"error":"no fixture for this URL"}`)),
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// amazonFacetBody renders a minimal wire-shaped facet response.
func amazonFacetBody(categories, countries string) string {
	return `{"error":null,"hits":10000,"facets":{"business_category_facet":[` + categories +
		`],"normalized_country_code_facet":[` + countries + `]},"jobs":[{}]}`
}

// amazonJobs renders count filler jobs starting at the given id.
func amazonJobs(firstID, count int) string {
	items := make([]string, 0, count)

	for i := range count {
		id := firstID + i
		items = append(items, fmt.Sprintf(
			`{"id_icims":"%d","title":"Engineer %d","job_path":"/en/jobs/%d/engineer-%d","normalized_location":"Seattle, Washington, USA","job_schedule_type":"full-time","posted_date":"June 17, 2026"}`,
			id, id, id, id))
	}

	return strings.Join(items, ",")
}

// TestAmazonParsesPostings walks a one-category board whose slice page is the
// real bytes of the business_category=compensation slice captured 2026-07-30.
func TestAmazonParsesPostings(t *testing.T) {
	t.Parallel()

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):                amazonFacetBody(`{"compensation": 2}`, `{"IND": 2}`),
		amazonSearchURL(0, 100, "compensation", "", false): icimsFixture(t, "amazon_search_compensation_slice.json"),
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	first := postings[0]
	test.Eq(t, "amazon", first.Company)
	test.Eq(t, "https://www.amazon.jobs/en/jobs/10464593/senior-compensation-consultant-apac-meatr-operations-customer-service", first.URL)
	test.Eq(t, "Senior Compensation Consultant, APAC/MEATR Operations/Customer Service", first.Title)
	test.Eq(t, "Bengaluru, Karnataka, IND", first.Location)
	test.Eq(t, "Human Resources", first.Department)
	test.Eq(t, "10464593", first.ExternalID)
	test.Eq(t, internal.EmploymentTypeFullTime, first.EmploymentType)
	// The fixture publishes "July  2, 2026" with a space-padded day, which is
	// what the second layout in amazonPostedLayouts cannot read.
	test.Eq(t, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), first.PostedAt)
	test.Eq(t, internal.PostingSource{Platform: "amazon", Key: "amazon"}, first.Source)

	// No compensation from this platform: measured, not an oversight — see the
	// comment block in amazon.go.
	test.Nil(t, first.Compensation)
}

// TestAmazonFacetPartitionOnRealBytes pins the premise the whole walk stands
// on, against the facet response captured 2026-07-30: the business_category
// facet's buckets sum to exactly the country facet's sum (22,348 — the real
// board size, while "hits" saturates at the 10,000-row window), and the largest
// single bucket (aws) is inside the window with headroom.
func TestAmazonFacetPartitionOnRealBytes(t *testing.T) {
	t.Parallel()

	var doc amazonSearch
	must.NoError(t, json.Unmarshal([]byte(icimsFixture(t, "amazon_search_facets.json")), &doc))

	categories := amazonFacets(doc.Facets.BusinessCategory)
	countries := amazonFacets(doc.Facets.Country)

	test.Eq(t, 22348, amazonFacetTotal(categories))
	test.Eq(t, 22348, amazonFacetTotal(countries))
	test.Eq(t, 10000, doc.Hits, test.Sprint("hits saturates at the window; the facets carry the real size"))

	largest := 0
	for _, category := range categories {
		largest = max(largest, category.Count)
	}

	test.Eq(t, 7731, largest)
	test.Less(t, amazonMaxWindow, largest)
}

func TestAmazonPagesThroughASliceUntilShortPage(t *testing.T) {
	t.Parallel()

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):         amazonFacetBody(`{"aws": 150}`, `{"USA": 150}`),
		amazonSearchURL(0, 100, "aws", "", false):   `{"error":null,"hits":150,"jobs":[` + amazonJobs(1, 100) + `]}`,
		amazonSearchURL(100, 100, "aws", "", false): `{"error":null,"hits":150,"jobs":[` + amazonJobs(101, 50) + `]}`,
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	must.SliceEmpty(t, errs)
	test.Len(t, 150, postings)
	test.Len(t, 3, transport.requests)
}

// TestAmazonSubSlicesACategoryPastTheWindow: a category the size of the result
// window cannot be walked whole, so the walk fetches that category's country
// facet and walks the intersections instead.
func TestAmazonSubSlicesACategoryPastTheWindow(t *testing.T) {
	t.Parallel()

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):          amazonFacetBody(`{"aws": 12000}`, `{"USA": 11000}, {"IND": 1000}`),
		amazonSearchURL(0, 1, "aws", "", true):       amazonFacetBody(`{"aws": 12000}`, `{"USA": 11000}, {"IND": 1000}`),
		amazonSearchURL(0, 100, "aws", "USA", false): `{"error":null,"hits":11000,"jobs":[` + amazonJobs(1, 3) + `]}`,
		amazonSearchURL(0, 100, "aws", "IND", false): `{"error":null,"hits":1000,"jobs":[` + amazonJobs(1001, 2) + `]}`,
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	must.SliceEmpty(t, errs)
	test.Len(t, 5, postings)

	// The category itself must never have been walked unfiltered: any request
	// filtering on the category alone would reach for rows past the window.
	for _, url := range transport.requests {
		test.False(t, strings.Contains(url, "business_category%5B%5D=aws") &&
			!strings.Contains(url, "normalized_country_code") && !strings.Contains(url, "facets"),
			test.Sprintf("unsliced walk of an over-window category: %s", url))
	}
}

// TestAmazonNeverYieldsTheSamePostingTwice: pages reorder between requests and
// a recategorized job can land in two slices; the canonical URL is yielded
// once, exactly like the iCIMS walk.
func TestAmazonNeverYieldsTheSamePostingTwice(t *testing.T) {
	t.Parallel()

	shared := amazonJobs(1, 2)

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):           amazonFacetBody(`{"aws": 2}, {"finance": 2}`, `{"USA": 4}`),
		amazonSearchURL(0, 100, "aws", "", false):     `{"error":null,"jobs":[` + shared + `]}`,
		amazonSearchURL(0, 100, "finance", "", false): `{"error":null,"jobs":[` + shared + `]}`,
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	// The two facets sum equal (4 == 4), so no partition error either.
	must.SliceEmpty(t, errs)
	test.Len(t, 2, postings)
}

// TestAmazonStopsWhenTheBoardIgnoresItsOffset covers the failure mode
// [pageRepeatGuard] exists for.
func TestAmazonStopsWhenTheBoardIgnoresItsOffset(t *testing.T) {
	t.Parallel()

	page := `{"error":null,"jobs":[` + amazonJobs(1, 100) + `]}`

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):         amazonFacetBody(`{"aws": 9000}`, `{"USA": 9000}`),
		amazonSearchURL(0, 100, "aws", "", false):   page,
		amazonSearchURL(100, 100, "aws", "", false): page,
		amazonSearchURL(200, 100, "aws", "", false): page,
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	must.SliceEmpty(t, errs)
	test.Len(t, 100, postings)
	test.Len(t, 3, transport.requests)
}

// TestAmazonReportsTheServersOwnError: past the window or the page-size cap
// this API answers HTTP 200 with zero jobs and prose in its error field, so
// that field must surface as a failure rather than as an empty slice.
func TestAmazonReportsTheServersOwnError(t *testing.T) {
	t.Parallel()

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):       amazonFacetBody(`{"aws": 50}`, `{"USA": 50}`),
		amazonSearchURL(0, 100, "aws", "", false): `{"error":"Cannot return more than 10000 results at once","hits":0,"jobs":[]}`,
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"amazon"`)
	test.StrContains(t, errs[0].Error(), "Cannot return more than 10000 results")
}

// TestAmazonReportsALeakyPartition: the walk's coverage claim rests on
// business_category partitioning the whole board, and the response carries the
// evidence either way on every crawl. A mismatch must be reported — after the
// postings, so a drifting facet still contributes everything it served.
func TestAmazonReportsALeakyPartition(t *testing.T) {
	t.Parallel()

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):       amazonFacetBody(`{"aws": 1}`, `{"USA": 1}, {"IND": 1}`),
		amazonSearchURL(0, 100, "aws", "", false): `{"error":null,"jobs":[` + amazonJobs(1, 1) + `]}`,
	}}

	postings, errs := drain(Amazon(t.Context(), &http.Client{Transport: transport}, "amazon"))

	test.Len(t, 1, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "no longer partitions")
}

func TestAmazonReportsHTTPError(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"www.amazon.jobs": `{}`,
	})
	transport.status = http.StatusServiceUnavailable

	postings, errs := drain(Amazon(t.Context(), client, "amazon"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"amazon"`)
}

func TestAmazonReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"www.amazon.jobs": `{"jobs": [`,
	})

	postings, errs := drain(Amazon(t.Context(), client, "amazon"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"amazon"`)
}

func TestAmazonStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	transport := &amazonRouteTransport{pages: map[string]string{
		amazonSearchURL(0, 1, "", "", true):       amazonFacetBody(`{"aws": 5}`, `{"USA": 5}`),
		amazonSearchURL(0, 100, "aws", "", false): `{"error":null,"jobs":[` + amazonJobs(1, 5) + `]}`,
	}}

	var seen int

	for range Amazon(t.Context(), &http.Client{Transport: transport}, "amazon") {
		seen++

		break
	}

	test.Eq(t, 1, seen)
}

func TestAmazonTime(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		value string
		want  time.Time
	}{
		{"June 17, 2026", time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)},
		// The API space-pads single-digit days.
		{"July  2, 2026", time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
		{"", time.Time{}},
		{"5 minutes", time.Time{}},
	} {
		test.Eq(t, tc.want, amazonTime(tc.value))
	}
}

// TestAmazonHostComesFromTheCandidateFile holds this platform to the same
// registry rule as every other: what is crawled is recorded, with its
// measurements, in testdata/candidates.
func TestAmazonHostComesFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "amazon_endpoints.txt")

	test.True(t, candidates[amazonHost],
		test.Sprintf("host %q is not in testdata/candidates/amazon_endpoints.txt", amazonHost))
}

// TestAmazonSharesOnePacingKey asserts the registration in this file's init
// actually took: ~300 sequential page requests against one hostname is the
// www.google.com shape, and unpaced it is the shape that rate-limited 56
// Workable boards into looking dead.
func TestAmazonSharesOnePacingKey(t *testing.T) {
	t.Parallel()

	policy := httpx.ServicePolicyForHost(amazonHost, httpx.DefaultPerHostLimit)

	test.Eq(t, amazonPlatform, policy.Key)
	test.NotEq(t, time.Duration(0), policy.Interval, test.Sprint("a ~300-request walk of one host must be paced"))
}

// TestAmazonLive walks the whole live board.
func TestAmazonLive(t *testing.T) {
	t.Parallel()

	testSingle(t, "amazon", Amazon)
}

// TestAmazonURLConstruction pins the request URLs, which the exact-match
// transports above depend on.
func TestAmazonURLConstruction(t *testing.T) {
	t.Parallel()

	test.Eq(t,
		"https://www.amazon.jobs/en/search.json?base_query=&offset=0&result_limit=1&facets%5B%5D=business_category&facets%5B%5D=normalized_country_code",
		amazonSearchURL(0, 1, "", "", true))

	test.Eq(t,
		"https://www.amazon.jobs/en/search.json?base_query=&offset=200&result_limit=100&business_category%5B%5D=no-business-category&normalized_country_code%5B%5D=USA",
		amazonSearchURL(200, 100, "no-business-category", "USA", false))
}
