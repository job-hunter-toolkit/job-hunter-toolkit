package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// oracleCloudTestTenant is the triple the fixtures below are served for. Its
// site number is a bare "CX_1"; the named-site form ("AEO-Careers",
// "PenskeCareers") is covered by TestOracleCloudBuildsTheFinder.
const oracleCloudTestTenant = "acme,eluq.fa.us2.oraclecloud.com,CX_1"

// oracleCloudListFixture is a requisition list in the shape
// recruitingCEJobRequisitions returns: everything hangs off a single-element
// "items" array, with the total and the requisitions as siblings inside it.
//
// It is hand-written, and it is kept only for the shape variations the two live
// captures under testdata do not happen to contain: an Id arriving as a JSON
// number, a requisition with no enrichment fields at all, and values padded with
// whitespace. What the API actually sends is settled by
// TestOracleCloudParsesACapturedLiveSite below, not by this.
const oracleCloudListFixture = `{
	"items": [
		{
			"TotalJobsCount": 2,
			"requisitionList": [
				{
					"Id": "18234",
					"Title": "  Pharmacy Technician  ",
					"PrimaryLocation": "  Cincinnati, OH  ",
					"PostedDate": "2026-06-14",
					"JobSchedule": "Full time",
					"WorkplaceTypeCode": "ORA_HYBRID",
					"Department": "Pharmacy"
				},
				{
					"Id": 18235,
					"Title": "Store Associate",
					"PrimaryLocation": ""
				}
			]
		}
	]
}`

// oracleCloudFixture reads one of the captured live responses under testdata.
//
// Both captures are verbatim except that the requisition list is truncated to
// its first two entries: a full page is 200 requisitions and ~343 KB, and the
// 198 dropped ones say nothing the first two do not. Every key and every value
// that remains is Oracle's own, nulls included.
func oracleCloudFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// oracleCloudCaptureTransport serves a captured first page at offset 0 and an
// end-of-list page at every other offset.
//
// The captures keep the TotalJobsCount their sites really sent — 222 and 15,120
// — while holding only the two requisitions they were truncated to, so an
// adapter that believes the count will ask for more. Answering those with an
// empty requisition list is what the live site does past its last row, and it
// keeps the capture verbatim instead of editing its count down to match the
// truncation.
//
// It exists rather than fixtureClient because that helper appends to a slice
// without a lock, which was safe while every adapter paged serially. It is
// shared by other adapters' tests, so the behaviour is reproduced here rather
// than changed underneath them.
type oracleCloudCaptureTransport struct {
	capture string

	mu       sync.Mutex
	requests int
}

func (o *oracleCloudCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	o.mu.Lock()
	o.requests++
	o.mu.Unlock()

	body := o.capture

	for _, part := range strings.Split(oracleCloudFinder(req.URL.RawQuery), ",") {
		if value, ok := strings.CutPrefix(part, "offset="); ok && value != "0" {
			body = `{"items":[{"TotalJobsCount":0,"requisitionList":[]}]}`
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

func TestOracleCloudParsesRequisitions(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"eluq.fa.us2.oraclecloud.com": oracleCloudListFixture,
	})

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// The list response already carries everything this project stores, so the
	// per-posting detail request every other implementation of this API makes is
	// skipped entirely: two postings, one request.
	must.Len(t, 1, transport.requests)

	technician, associate := postings[0], postings[1]

	test.Eq(t, "acme", technician.Company)
	test.Eq(t, "Pharmacy Technician", technician.Title)
	test.Eq(t, "Cincinnati, OH", technician.Location)
	test.Eq(t, "Pharmacy", technician.Department)

	// From JobSchedule. JobType, which this adapter used to read instead, was
	// populated on none of the 6,780 requisitions sampled from live tenants.
	test.Eq(t, internal.EmploymentTypeFullTime, technician.EmploymentType)

	// ORA_HYBRID is Oracle's genuine three-state workplace field, which a
	// Remote *bool could not have expressed at all.
	test.Eq(t, internal.WorkplaceTypeHybrid, technician.WorkplaceType)

	test.Eq(t, time.Date(2026, time.June, 14, 0, 0, 0, 0, time.UTC), technician.PostedAt)
	test.Eq(t, "UTC", technician.PostedAt.Location().String())

	test.Eq(t, "18234", technician.ExternalID)

	// The employer's own requisition number is a different field on this
	// platform and is not corroborated as present in the list, so it stays
	// empty rather than being filled with the ATS's id.
	test.Eq(t, "", technician.RequisitionID)

	test.Eq(t,
		"https://eluq.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/18234",
		technician.URL,
	)

	test.Eq(t, internal.PostingSource{Platform: "oraclecloud", Key: oracleCloudTestTenant}, technician.Source)

	// A numeric Id must not become "18235.0" or "1.8235e+04" in a URL.
	test.Eq(t, "18235", associate.ExternalID)
	test.Eq(t,
		"https://eluq.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/18235",
		associate.URL,
	)

	// Absent enrichment is absent, never guessed.
	test.Eq(t, "unknown/remote", associate.Location)
	test.Eq(t, internal.EmploymentTypeUnknown, associate.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeUnknown, associate.WorkplaceType)
	test.True(t, associate.PostedAt.IsZero())
}

// TestOracleCloudBuildsTheFinder covers the query parameter that is a small
// language of its own.
//
// "findReqs;siteNumber=CX_1,limit=200,offset=0" carries its structure in a
// semicolon, commas and equals signs, all three of which url.QueryEscape
// escapes — which turns a working request into one Oracle answers with an error
// rather than with jobs.
func TestOracleCloudBuildsTheFinder(t *testing.T) {
	t.Parallel()

	tenant, err := parseOracleCloudTenant("aeo,hcml.fa.us2.oraclecloud.com,AEO-Careers")
	must.NoError(t, err)

	built := oracleCloudListURL(tenant, 400)

	test.StrContains(t, built, "finder=findReqs;siteNumber=AEO-Careers,limit=200,offset=400,sortBy=POSTING_DATES_DESC")
	test.StrContains(t, built, "https://hcml.fa.us2.oraclecloud.com/hcmRestApi/resources/latest/recruitingCEJobRequisitions")
	test.StrContains(t, built, "onlyData=true")

	// It still has to be a URL: a site number carrying a character that would
	// otherwise end the parameter must be escaped, not passed through. An
	// unescaped "&" would silently truncate the finder to "siteNumber=Site Name"
	// and Oracle would answer for a site that does not exist.
	parsed, err := url.Parse(oracleCloudListURL(oracleCloudTenant{host: "h", site: "Site Name&x"}, 0))
	must.NoError(t, err)
	test.StrContains(t, parsed.RawQuery, "siteNumber=Site%20Name%26x,")
	test.Eq(t, "findReqs;siteNumber=Site Name&x,limit=200,offset=0,sortBy=POSTING_DATES_DESC", oracleCloudFinder(parsed.RawQuery))
}

// oracleCloudFinder pulls the finder value out of a request URL's raw query.
//
// Deliberately not through [net/url.Values]: Go's query parser treats a
// semicolon as an error and drops the pair that contains one, and this
// parameter's syntax is built on a semicolon, so Query().Get("finder") is always
// empty here. That is a property of the client library rather than of Oracle,
// which reads the parameter it is sent — but a test that reaches for Query()
// asserts nothing at all, and this file learned that the hard way.
func oracleCloudFinder(rawQuery string) string {
	for _, part := range strings.Split(rawQuery, "&") {
		value, ok := strings.CutPrefix(part, "finder=")
		if !ok {
			continue
		}

		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return value
		}

		return decoded
	}

	return ""
}

func TestOracleCloudFinderEscape(t *testing.T) {
	t.Parallel()

	// The three structural characters survive; everything unsafe does not.
	test.Eq(t, "findReqs;siteNumber=CX_1,limit=200", oracleCloudFinderEscape("findReqs;siteNumber=CX_1,limit=200"))
	test.Eq(t, "AEO-Careers", oracleCloudFinderEscape("AEO-Careers"))
	test.Eq(t, "a%20b", oracleCloudFinderEscape("a b"))
	test.Eq(t, "a%26b", oracleCloudFinderEscape("a&b"))
	test.Eq(t, "a%2Fb%3Fc%23d", oracleCloudFinderEscape("a/b?c#d"))
}

// oracleCloudPage builds a response of exactly count requisitions whose ids
// carry the given prefix, so a full page can be served without the short-page
// check being what ends a pagination loop under test.
func oracleCloudPage(prefix string, count, total int) string {
	requisitions := make([]string, count)

	for i := range requisitions {
		requisitions[i] = fmt.Sprintf(`{"Id":"%s%d","Title":"Job %s%d","PrimaryLocation":"Remote"}`, prefix, i, prefix, i)
	}

	return fmt.Sprintf(`{"items":[{"TotalJobsCount":%d,"requisitionList":[%s]}]}`, total, strings.Join(requisitions, ","))
}

// oracleCloudOffsetTransport serves pages keyed by the offset inside the finder
// parameter, which is the only place this API states one.
//
// It is mutex-guarded because this adapter fetches pages concurrently
// ([oracleCloudPageFetchers]); an unguarded counter here would make every
// pagination test in this file a data race rather than an assertion.
type oracleCloudOffsetTransport struct {
	// total is the site's TotalJobsCount, and how many requisitions are served
	// across all pages.
	total int

	// distinct makes every page unique regardless of the total, so only a hard
	// ceiling can end the walk.
	distinct bool

	// perPage caps how many requisitions a page holds regardless of the limit
	// asked for, which is what a server-side page cap looks like. Zero means the
	// requested page size.
	perPage int

	// window models the platform's measured deep-paging wall: an offset whose
	// page would reach past it is answered with no requisitions and a zeroed
	// total, which is what Oracle really does. Zero disables it.
	window int

	// rows is how many requisitions the site really holds, when that differs
	// from the total it reports. A site whose TotalJobsCount overshoots what it
	// will serve is the ordinary case for this platform: the count is a search
	// estimate, and two requests to the same site minutes apart returned 15119
	// and 15120. Zero means the site serves exactly what it reports.
	rows int

	// ignoreOffset serves the identical first page for every offset, which is
	// the misbehaviour pageRepeatGuard exists to catch.
	//
	// The package's shared repeatingPageClient does the same thing, but its
	// transport counts requests without a lock, which was safe while every
	// adapter paged serially and is a data race now that this one does not. It
	// belongs to another adapter's test file, so the behaviour is reproduced
	// here rather than that helper being changed underneath its owners.
	ignoreOffset bool

	mu       sync.Mutex
	requests int
	offsets  []int
}

func (o *oracleCloudOffsetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	offset := 0

	for _, part := range strings.Split(oracleCloudFinder(req.URL.RawQuery), ",") {
		if value, ok := strings.CutPrefix(part, "offset="); ok {
			offset, _ = strconv.Atoi(value)
		}
	}

	o.mu.Lock()
	o.requests++
	o.offsets = append(o.offsets, offset)
	o.mu.Unlock()

	count := oracleCloudPageSize
	if o.perPage > 0 {
		count = o.perPage
	}

	if !o.distinct {
		served := o.total
		if o.rows > 0 {
			served = o.rows
		}

		if remaining := served - offset; remaining < count {
			count = max(remaining, 0)
		}
	}

	total := o.total

	// Past the wall Oracle answers HTTP 200 with an empty list AND a zeroed
	// count, which is indistinguishable from a board with nothing open.
	if o.window > 0 && offset+oracleCloudPageSize > o.window {
		count, total = 0, 0
	}

	prefix := strconv.Itoa(offset) + "-"
	if o.ignoreOffset {
		prefix, count = "", oracleCloudPageSize
	}

	body := oracleCloudPage(prefix, count, total)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// count reports how many requests the transport served.
func (o *oracleCloudOffsetTransport) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.requests
}

// TestOracleCloudPaginatesToTheTotal walks a tenant bigger than one page, which
// is the normal case for this platform: the largest registered tenant publishes
// about 16,300 requisitions, or 82 pages.
func TestOracleCloudPaginatesToTheTotal(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: oracleCloudPageSize + 30}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	test.Len(t, oracleCloudPageSize+30, postings)
	test.Eq(t, 2, transport.count())
}

// TestOracleCloudStopsOnTheReportedTotal covers the cheaper of the two stopping
// conditions: a site whose last page happens to be exactly full is finished, and
// asking for the page after it is a wasted request against a shared host.
func TestOracleCloudStopsOnTheReportedTotal(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: oracleCloudPageSize}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	test.Len(t, oracleCloudPageSize, postings)
	test.Eq(t, 1, transport.count())
}

// TestOracleCloudKeepsWalkingPastAServerSidePageCap guards against the quietest
// way this adapter could fail.
//
// A short page is the usual signal that a board is finished, but several boards
// in this ecosystem answer with fewer rows than the limit asked for and still
// expect the caller to keep going — ADP's public API is documented to do exactly
// that. Stopping on the first short page would publish 50 of Kroger's ~16,300
// requisitions and report success, which no downstream check could catch.
func TestOracleCloudKeepsWalkingPastAServerSidePageCap(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: 130, perPage: 50}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)

	// 50 + 50 + 30: the offset advances by what each page actually held, so no
	// row is skipped and none is fetched twice.
	test.Len(t, 130, postings)
	test.Eq(t, 3, transport.count())

	seen := make(map[string]bool, len(postings))
	for _, posting := range postings {
		test.False(t, seen[posting.URL], test.Sprintf("posting %q was yielded twice", posting.URL))
		seen[posting.URL] = true
	}
}

// TestOracleCloudStopsWhenTheSiteIgnoresOffset is a regression test for the
// failure this package has just finished repairing in eight adapters: a tenant
// that answers every offset with the same first page never sends a short one, so
// a loop that ends only on a short page runs until the crawl deadline, pinning a
// worker and hammering one host, while internal.Dedupe hides the duplicates.
func TestOracleCloudStopsWhenTheSiteIgnoresOffset(t *testing.T) {
	t.Parallel()

	// A total large enough that the count can never end the walk either.
	transport := &oracleCloudOffsetTransport{total: 1_000_000, ignoreOffset: true}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)

	// Exactly one page of postings is what matters: the repeat is caught before
	// any duplicate is yielded, which is what keeps internal.Dedupe from quietly
	// absorbing them.
	test.Len(t, oracleCloudPageSize, postings)

	// The request count is a range rather than a number, because pages are
	// fetched concurrently. The ceiling is reasoned, not fitted: one first page,
	// plus the oracleCloudPageFetchers that can be in flight when the repeat is
	// spotted, plus at most one more that the scheduler can start in the window
	// between a fetcher's slot being released and the stop signal being seen.
	// What the bound is really asserting is that a tenant ignoring "offset"
	// costs a handful of requests rather than walking the full 50-page window.
	test.GreaterEq(t, 2, transport.count())
	test.LessEq(t, 2+oracleCloudPageFetchers, transport.count())
	test.Less(t, oracleCloudMaxWindow/oracleCloudPageSize, transport.count())
}

// TestOracleCloudStopsAtTheDeepPagingWindow is the measured behaviour of the
// nine candidate tenants with more than 10,000 open requisitions.
//
// Oracle refuses offset+limit past oracleCloudMaxWindow and answers with an
// empty list AND a zeroed TotalJobsCount — an HTTP 200 that looks exactly like a
// board with nothing open. Bisected live against Kroger, reproduced on Marriott
// and AutoZone. Two things must hold: the walk stops at the wall rather than
// spending the rest of the crawl on requests that return nothing, and reaching
// it is NOT an error, because a truncated 15,000-req employer is still 10,000
// real postings and flagging it would push the Source Health workflow toward its
// failure alarm for a platform working as documented.
func TestOracleCloudStopsAtTheDeepPagingWindow(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{
		total:  15_119,
		window: oracleCloudMaxWindow,
	}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)

	// Fifty pages of 200: the last one asked for is offset=9800, whose page ends
	// exactly on the wall. offset=9900 is never requested, because it is the
	// request Oracle refuses.
	test.Len(t, oracleCloudMaxWindow, postings)
	test.Eq(t, oracleCloudMaxWindow/oracleCloudPageSize, transport.count())

	transport.mu.Lock()
	defer transport.mu.Unlock()

	for _, offset := range transport.offsets {
		test.LessEq(t, oracleCloudMaxWindow, offset+oracleCloudPageSize,
			test.Sprintf("offset %d would straddle the deep-paging wall and be answered with nothing", offset))
	}
}

// TestOracleCloudStopsAtItsPageCeiling covers the backstop for the case neither
// a repeated page nor the window can catch: a site inside the window that serves
// so few rows per page that oracleCloudMaxPages requests do not exhaust it.
// Hitting that ceiling is reported rather than passed off as the end of a board,
// because unlike the window it is a shape nobody here has measured.
func TestOracleCloudStopsAtItsPageCeiling(t *testing.T) {
	t.Parallel()

	// One row per page inside a 10,000-row window is 10,000 pages' worth of
	// work, twenty times the backstop.
	transport := &oracleCloudOffsetTransport{total: 1_000_000, perPage: 1}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	test.Eq(t, oracleCloudMaxPages, transport.count())
	test.Len(t, oracleCloudMaxPages, postings)

	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "refusing to keep paginating")
}

// TestOracleCloudDoesNotReportACeilingItDidNotHit is the regression test for
// the first bug the live captures found in this rewrite.
//
// Page offsets are planned up front from the site's own TotalJobsCount, so a
// site that over-reports — Kroger's capture says 15,120 on a page holding two
// requisitions — plans more pages than oracleCloudMaxPages allows and would
// have been reported as outrunning the backstop even though it ran out of rows
// on its second request. That turns a source which finished cleanly into a
// failing one, and at 1,203 registered tenants a false failure mode is exactly
// what pushes the Source Health workflow toward its 35% alarm.
func TestOracleCloudDoesNotReportACeilingItDidNotHit(t *testing.T) {
	t.Parallel()

	// Two rows per page against a claimed 100,000 plans far past the backstop;
	// the site then runs out after four rows, which is the Kroger capture's
	// shape without its size.
	transport := &oracleCloudOffsetTransport{total: 100_000, perPage: 2, rows: 4}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	test.SliceNotEmpty(t, postings)
}

// TestOracleCloudFetchesPagesConcurrently is the whole point of the fan-out, and
// the reason it is correct here and would not be on Greenhouse: Oracle gives
// every tenant its own Fusion Applications host, so httpx keys the rate limiter
// per employer and pages of one employer do not queue behind the rest of the
// platform.
func TestOracleCloudFetchesPagesConcurrently(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudConcurrencyTransport{total: oracleCloudPageSize * 12}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	test.Len(t, oracleCloudPageSize*12, postings)

	transport.mu.Lock()
	defer transport.mu.Unlock()

	test.Greater(t, 1, transport.peak, test.Sprint("pages after the first should overlap, not run one at a time"))

	// The bound is the politeness contract: it is deliberately equal to httpx's
	// per-service limit, so this adapter can never be the reason a tenant sees
	// more in-flight requests than the limiter allows.
	test.LessEq(t, oracleCloudPageFetchers, transport.peak)
}

// oracleCloudConcurrencyTransport records the high-water mark of overlapping
// requests. Each request blocks briefly so that pages issued together actually
// overlap in time rather than being serialised by how fast a stub can answer.
type oracleCloudConcurrencyTransport struct {
	total int

	mu       sync.Mutex
	inFlight int
	peak     int
}

func (o *oracleCloudConcurrencyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	o.mu.Lock()
	o.inFlight++
	o.peak = max(o.peak, o.inFlight)
	o.mu.Unlock()

	time.Sleep(2 * time.Millisecond)

	defer func() {
		o.mu.Lock()
		o.inFlight--
		o.mu.Unlock()
	}()

	offset := 0

	for _, part := range strings.Split(oracleCloudFinder(req.URL.RawQuery), ",") {
		if value, ok := strings.CutPrefix(part, "offset="); ok {
			offset, _ = strconv.Atoi(value)
		}
	}

	count := min(oracleCloudPageSize, max(o.total-offset, 0))

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(oracleCloudPage(strconv.Itoa(offset)+"-", count, o.total))),
		Request:    req,
	}, nil
}

// TestOracleCloudStopsWhenTheConsumerDoes guards the iterator contract the
// health command depends on: it caps each source at 100 postings by returning
// false from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
func TestOracleCloudStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: 1_000_000, ignoreOffset: true}

	var seen int

	for range OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant) {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)

	// The first page is emitted before any fan-out is scheduled, so a consumer
	// that stops inside it costs exactly one request — the fan-out must not
	// front-run the pages it is meant to follow.
	test.Eq(t, 1, transport.count())
}

// TestOracleCloudReportsAnUnreadableResponse covers the shapes that must never
// be mistaken for an employer with no openings. A silently-empty source is the
// worst failure this project has, and every case here answers HTTP 200.
func TestOracleCloudReportsAnUnreadableResponse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want string
	}{
		// The envelope is always one item, even for a site with no jobs, so an
		// empty array is Oracle answering something other than this API.
		"no items": {
			body: `{"items":[]}`,
			want: "no items in the requisition list response",
		},
		// Neither a list nor a count means the shape is not the one this
		// adapter was written against.
		"neither a list nor a count": {
			body: `{"items":[{}]}`,
			want: "layout may have changed",
		},
		"a renamed requisition list": {
			body: `{"items":[{"requisitions":[{"Id":"1"}]}]}`,
			want: "layout may have changed",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, _ := fixtureClient(map[string]string{"eluq.fa.us2.oraclecloud.com": tc.body})

			postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

			test.SliceEmpty(t, postings)
			must.Len(t, 1, errs)
			must.StrContains(t, errs[0].Error(), tc.want)

			// Among ~1,800 sources an error that does not name its tenant is
			// unattributable.
			must.StrContains(t, errs[0].Error(), "acme")
			must.StrContains(t, errs[0].Error(), "CX_1")
		})
	}
}

// TestOracleCloudAcceptsAnEmptySite is the other half of that rule: a site that
// answers with this API's envelope and reports zero jobs is a careers site with
// nothing open, which is not an error.
func TestOracleCloudAcceptsAnEmptySite(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"eluq.fa.us2.oraclecloud.com": `{"items":[{"TotalJobsCount":0,"requisitionList":[]}]}`,
	})

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

func TestOracleCloudReportsANon200(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"eluq.fa.us2.oraclecloud.com": `{}`},
		status: http.StatusInternalServerError,
	}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestOracleCloudSurvivesRetypedFields is the Jibe "meta_data" lesson applied
// before it costs anything: fetchJSON decodes a whole page at once, so one field
// arriving with an unexpected JSON type takes down every posting on it. The
// fields whose type nobody here has confirmed against a real response are
// therefore `any`, and this is what proves it.
func TestOracleCloudSurvivesRetypedFields(t *testing.T) {
	t.Parallel()

	const retyped = `{"items":[{"TotalJobsCount":"1","requisitionList":[
		{"Id":9001,"Title":"Analyst","PrimaryLocation":"Remote","PostedDate":null,
		 "JobType":["Full time"],"WorkplaceTypeCode":{"code":"ORA_REMOTE"},"JobFunction":false}
	]}]}`

	client, transport := fixtureClient(map[string]string{"eluq.fa.us2.oraclecloud.com": retyped})

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	// A count that arrives as a quoted number is still a count, so the walk ends
	// on it rather than asking for a page that does not exist.
	must.Len(t, 1, transport.requests)

	test.Eq(t, "9001", postings[0].ExternalID)
	test.Eq(t, "Analyst", postings[0].Title)

	// A single-element array is read as the value it wraps, which is what
	// anyText already does for BambooHR.
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)

	// An object is not a scalar, so it renders as nothing rather than as Go's
	// spelling of a map. Absent beats wrong: "map[code:ORA_REMOTE]" would not
	// normalize to a workplace type anyway, but it would render into a
	// department or a location on the next field that is typed this way.
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[0].WorkplaceType)
	test.Eq(t, "", postings[0].Department)

	// A null date is no date, not the epoch.
	test.True(t, postings[0].PostedAt.IsZero())
}

func TestOracleCloudRejectsAMalformedTenant(t *testing.T) {
	t.Parallel()

	badKeys := []string{
		"acme",
		"acme,eluq.fa.us2.oraclecloud.com",
		"acme,eluq.fa.us2.oraclecloud.com,CX_1,extra",
		"acme,,CX_1",
		"acme,eluq.fa.us2.oraclecloud.com,",
	}

	for _, key := range badKeys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			client, transport := fixtureClient(map[string]string{"oraclecloud.com": oracleCloudListFixture})

			postings, errs := drain(OracleCloud(t.Context(), client, key))

			test.SliceEmpty(t, postings)
			test.SliceEmpty(t, transport.requests)
			must.Len(t, 1, errs)
			must.StrContains(t, errs[0].Error(), "invalid Oracle Cloud tenant")

			test.Eq(t, key, oracleCloudCompanyName(key))
		})
	}
}

// TestOracleCloudAddsNoDoubleCountedEmployer is the twin of the SuccessFactors
// check, and the one that found a real overlap: Marriott is the second-largest
// tenant in the Oracle candidate file and was already registered on Jibe, so
// adding it would have counted ~11,900 postings twice in a trend line that
// cannot tell that apart from hiring.
func TestOracleCloudAddsNoDoubleCountedEmployer(t *testing.T) {
	t.Parallel()

	elsewhere := companiesOnOtherPlatforms(oracleCloudPlatform)

	for _, key := range OracleCloudTenants {
		company := oracleCloudCompanyName(key)

		platform, clash := elsewhere[strings.ToLower(company)]

		test.False(t, clash, test.Sprintf("company %q is registered on both oraclecloud and %s, so its postings would be counted twice; pick one route", company, platform))
	}
}

// TestOracleCloudTenantsComeFromTheCandidateFile keeps the registered list
// honest about its own provenance, for the reasons spelled out on its
// SuccessFactors twin: the registered set is a hand-picked staging subset of a
// much larger unprobed candidate file, and a triple that is not in that file was
// either typed from memory or edited after the fact.
func TestOracleCloudTenantsComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateTenants(t, "oracle_orc_tenants.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(OracleCloudTenants))

	for _, key := range OracleCloudTenants {
		tenant, err := parseOracleCloudTenant(key)
		must.NoError(t, err, must.Sprintf("registered tenant %q", key))

		test.False(t, seen[tenant.slug], test.Sprintf("company %q is registered twice", tenant.slug))
		seen[tenant.slug] = true

		test.True(t, candidates[key], test.Sprintf("registered tenant %q is not in testdata/candidates/oracle_orc_tenants.txt", key))

		// Every registered host is under one suffix, which is what lets a single
		// servicePolicyFor entry keep the whole platform inside one politeness
		// budget. A tenant on some other host would silently escape it.
		test.StrHasSuffix(t, ".oraclecloud.com", tenant.host)
	}

	test.Less(t, len(candidates), len(OracleCloudTenants), test.Sprint("the registered list should stay a subset of the candidates"))
}

// TestOracleCloudParsesACapturedLiveSite is the fixture that decides whether
// this adapter reads Oracle Recruiting Cloud, as opposed to reading the shape a
// document said it has. The body is the first page of
// fa-eomf-saasfaprod1.fa.ocs.oraclecloud.com site CX_1002 (UT Health San
// Antonio) as captured on 2026-07-28, truncated to two requisitions.
//
// What the capture establishes, and what the hand-written fixture above could
// not:
//
//   - Employment type lives in JobSchedule. This adapter was written to read
//     JobType, from docs/research/ats-platform-survey.md's field list. Across
//     6,780 requisitions sampled from 1,501 live tenants JobType was populated
//     zero times, so every registered tenant's employment type was silently
//     empty while the adapter looked healthy.
//
//   - WorkplaceTypeCode is in the LIST response, on 30% of sampled
//     requisitions. The survey files it under "detail-only fields", which would
//     have argued for a per-posting request to fetch something already in hand.
//
//   - Its live spelling is ORA_ON_SITE, not the ORA_ONSITE the survey and this
//     adapter's own comment recorded.
//
//   - Department is a real list field, and it is more specific than the
//     JobFunction the adapter used to read.
func TestOracleCloudParsesACapturedLiveSite(t *testing.T) {
	t.Parallel()

	const tenant = "uthealthsa,fa-eomf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1002"

	transport := &oracleCloudCaptureTransport{capture: oracleCloudFixture(t, "oracle_uthealthsa_requisitions.json")}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, tenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	first := postings[0]

	test.Eq(t, "uthealthsa", first.Company)
	test.Eq(t, "Histology Technician-Lead (OHOPD Medicine Dermatology MCC)", first.Title)
	test.Eq(t, "San Antonio, TX, United States", first.Location)
	test.Eq(t, "7237", first.ExternalID)
	test.Eq(t, "O9308 - HOPD Medicine Dermatology MCC", first.Department)
	test.Eq(t, internal.EmploymentTypeFullTime, first.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeOnsite, first.WorkplaceType)
	test.Eq(t, time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC), first.PostedAt)

	test.Eq(t,
		"https://fa-eomf-saasfaprod1.fa.ocs.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1002/job/7237",
		first.URL,
	)
}

// TestOracleCloudParsesASparseCapturedSite is the other half of the live shape,
// and the more common one. The body is the first page of
// eluq.fa.us2.oraclecloud.com site CX_2001 (Kroger, the largest tenant in the
// candidate file) as captured on 2026-07-28, truncated to two requisitions.
//
// Almost every enrichment field is an explicit JSON null on this tenant —
// WorkplaceTypeCode, JobFunction, Department, JobFamily, JobType, ContractType —
// which is what the majority of the 1,203 registered tenants look like. Two
// things have to hold: nulls become absent fields rather than the string "null"
// or a zero date, and the three fields this project actually needs are still
// there.
func TestOracleCloudParsesASparseCapturedSite(t *testing.T) {
	t.Parallel()

	const tenant = "kroger,eluq.fa.us2.oraclecloud.com,CX_2001"

	transport := &oracleCloudCaptureTransport{capture: oracleCloudFixture(t, "oracle_kroger_requisitions.json")}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, tenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	first := postings[0]

	test.Eq(t, "Night Stocker Clerk", first.Title)
	test.Eq(t, "Montrose, CO, United States", first.Location)
	test.Eq(t, "203341", first.ExternalID)
	test.Eq(t, time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC), first.PostedAt)

	// JobSchedule is present where every other enrichment field is null, which
	// is precisely why reading JobType instead cost this platform its employment
	// type entirely.
	test.Eq(t, internal.EmploymentTypePartTime, first.EmploymentType)

	// A JSON null is absent, not a value.
	test.Eq(t, "", first.Department)
	test.Eq(t, internal.WorkplaceTypeUnknown, first.WorkplaceType)

	// This capture carries the real 15,120 count on a page truncated to two
	// requisitions, so the adapter is right to keep asking; the transport
	// answers past offset 0 the way the live site answers past its last row.
	// What must not happen is the count being ignored, which would publish the
	// first page of a 15,000-req employer as the whole of it.
	test.Greater(t, 1, transport.requests)

	test.Eq(t,
		"https://eluq.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_2001/job/203341",
		first.URL,
	)
}

// TestOracleCloudCapturesCarryTheDocumentedFields fails if a capture is
// re-taken and quietly loses the fields the tests above assert on, which would
// turn those tests into assertions about nothing.
func TestOracleCloudCapturesCarryTheDocumentedFields(t *testing.T) {
	t.Parallel()

	for name, want := range map[string][]string{
		"oracle_uthealthsa_requisitions.json": {"Id", "Title", "PrimaryLocation", "PostedDate", "JobSchedule", "WorkplaceTypeCode", "WorkplaceType", "Department"},
		"oracle_kroger_requisitions.json":     {"Id", "Title", "PrimaryLocation", "PostedDate", "JobSchedule", "JobType", "WorkplaceTypeCode"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var envelope struct {
				Items []struct {
					TotalJobsCount  int                          `json:"TotalJobsCount"`
					RequisitionList []map[string]json.RawMessage `json:"requisitionList"`
				} `json:"items"`
			}

			must.NoError(t, json.Unmarshal([]byte(oracleCloudFixture(t, name)), &envelope))
			must.Len(t, 1, envelope.Items)
			must.Greater(t, 0, envelope.Items[0].TotalJobsCount)
			must.SliceNotEmpty(t, envelope.Items[0].RequisitionList)

			for _, key := range want {
				_, ok := envelope.Items[0].RequisitionList[0][key]
				test.True(t, ok, test.Sprintf("capture %s lost the %q key", name, key))
			}
		})
	}
}

// TestOracleCloudFixtureMatchesTheDecodedShape keeps the fixture honest: it is
// the only hand-written description of this API in the repository, so a typo in
// it would be invisible and would make every other test in this file pass
// against a shape the real service never sends.
func TestOracleCloudFixtureMatchesTheDecodedShape(t *testing.T) {
	t.Parallel()

	var envelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}

	must.NoError(t, json.Unmarshal([]byte(oracleCloudListFixture), &envelope))
	must.Len(t, 1, envelope.Items)

	for _, key := range []string{"TotalJobsCount", "requisitionList"} {
		_, ok := envelope.Items[0][key]
		test.True(t, ok, test.Sprintf("fixture item is missing %q", key))
	}
}
