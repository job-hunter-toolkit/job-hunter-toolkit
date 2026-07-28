package services

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/net/html"
)

// icimsFixture reads a search page captured from a live iCIMS classic career
// portal.
//
// The five captures under testdata are byte-for-byte what
// https://{host}/jobs/search?pr=0&in_iframe=1 returned on 2026-07-28, with
// nothing removed. They are kept whole rather than trimmed to a few cards
// because what varies between tenants is the page, not the card: which of the
// two field shapes carries the location, whether the header row exists at all,
// and whether <link rel="next"> is present are all page-level facts, and a
// hand-trimmed fixture is exactly where they would be lost.
//
// Two of the five, careers-gdms and clinical-emory, are boards this project
// crawls through Jibe instead (see [ICIMSHosts]) and so are not in the
// registry. They are kept here anyway: what they are being used for is their
// card template, and each is the only capture measured that exercises one --
// gdms is the only one with a posted date next to a pay field, and emory is the
// only one with no header row at all.
func icimsFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// icimsPageTransport serves one body per exact request URL.
//
// The shared fixtureTransport matches on a substring, and every page of one
// board shares "/jobs/search", so a paginated fixture built on it would serve
// whichever route the map happened to iterate to first.
type icimsPageTransport struct {
	pages    map[string]string
	requests []string
}

func (tr *icimsPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.requests = append(tr.requests, req.URL.String())

	body, ok := tr.pages[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     http.StatusText(http.StatusNotFound),
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("no fixture")),
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// icimsSinglePage serves one captured page as a complete board, by stripping the
// <link rel="next"> the live page carried.
//
// A capture of page 0 of a multi-page board advertises page 1, which the fixture
// map does not hold, so the walk would end on an HTTP 404 error rather than on
// the board running out. That would make every one of these tests assert on an
// error path. Removing the element reproduces what the board's own last page
// looks like, which is the state under test.
func icimsSinglePage(t *testing.T, name string) string {
	t.Helper()

	body := icimsFixture(t, name)

	start := strings.Index(body, `<link rel="next"`)
	if start < 0 {
		return body
	}

	end := strings.Index(body[start:], ">")
	must.Greater(t, 0, end, must.Sprint("the rel=next element in the capture is unterminated"))

	return body[:start] + body[start+end+1:]
}

// icimsBoard runs the adapter over one captured page served as a whole board.
func icimsBoard(t *testing.T, host, fixture string) []*internal.JobPosting {
	t.Helper()

	transport := &icimsPageTransport{pages: map[string]string{
		icimsSearchURL(host, 0): icimsSinglePage(t, fixture),
	}}

	postings, errs := drain(ICIMS(t.Context(), &http.Client{Transport: transport}, host))

	must.SliceEmpty(t, errs)
	must.SliceLen(t, 1, transport.requests, must.Sprint("a board with no rel=next should cost exactly one request"))

	return postings
}

// TestICIMSParsesEveryTenantTemplate asserts the first posting of each captured
// board, field by field.
//
// One test per tenant rather than one shared assertion, because the five
// captures were chosen for the ways they differ and a shared assertion would
// have to be weak enough to pass all of them.
func TestICIMSParsesEveryTenantTemplate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		host    string
		fixture string
		count   int
		first   internal.JobPosting
	}{
		{
			// The richest template measured: a header row carrying a posted date
			// with a machine-readable timestamp in its title attribute, and a
			// configured field block carrying ID, Job Location, Required
			// Clearance and Category.
			name:    "gdms",
			host:    "careers-gdms.icims.com",
			fixture: "icims_gdms_search_page1.html",
			count:   20,
			first: internal.JobPosting{
				Company:       "gdms",
				URL:           "https://careers-gdms.icims.com/jobs/73835/skillbridge---embedded-software-engineer/job",
				Title:         "Skillbridge - Embedded Software Engineer",
				Location:      "US-VA-Fairfax Station",
				Department:    "Various",
				RequisitionID: "2026-73835",
				ExternalID:    "73835",
				PostedAt:      time.Date(2026, time.July, 28, 14, 40, 0, 0, time.UTC),
			},
		},
		{
			// The day-first tenant. Its dates are "23/07/2026 08:17" on a
			// 24-hour clock, and parsing them month-first would either fail or,
			// on an ambiguous day, be five months wrong.
			name:    "tfghospitality",
			host:    "careers-tfghospitality.icims.com",
			fixture: "icims_tfghospitality_search_page1.html",
			count:   20,
			first: internal.JobPosting{
				Company:       "tfghospitality",
				URL:           "https://careers-tfghospitality.icims.com/jobs/8149/commis-ii/job",
				Title:         "Commis II",
				Location:      "Silver Sands Beach",
				RequisitionID: "2026-8149",
				ExternalID:    "8149",
				PostedAt:      time.Date(2026, time.July, 23, 8, 17, 0, 0, time.UTC),
			},
		},
		{
			// A board that fits on one page, so the live capture carries no
			// rel=next at all. Also the only tenant measured whose title label is
			// "Advertised Job Title", which is why the title comes from the
			// anchor's <h3> rather than from a label lookup.
			name:    "matchretail",
			host:    "careers-wow.icims.com",
			fixture: "icims_matchretail_search_page1.html",
			count:   19,
			first: internal.JobPosting{
				Company:       "wow",
				URL:           "https://careers-wow.icims.com/jobs/10493/sales-associate-part-time---centerpoint-mall/job",
				Title:         "Sales Associate Part Time - Centerpoint Mall",
				Location:      "CA-ON-Toronto",
				RequisitionID: "2026-10493",
				ExternalID:    "10493",
			},
		},
		{
			// The "Location : Address" trap. This card carries both
			// "Job Locations" = "US-TX-Seguin" and "Location : Address" =
			// "1500 E Court St", and a rule that matched any label containing
			// "location" would publish the street address as the location.
			name:    "petsuppliesplus",
			host:    "careers-petsuppliesplus.icims.com",
			fixture: "icims_petsuppliesplus_search_page1.html",
			count:   20,
			first: internal.JobPosting{
				Company:    "petsuppliesplus",
				URL:        "https://careers-petsuppliesplus.icims.com/jobs/18075/groomer/job",
				Title:      "Groomer",
				Location:   "US-TX-Seguin",
				Department: "Store Grooming",
				ExternalID: "18075",
			},
		},
		{
			// No header row at all: the location is "Campus Location" inside the
			// configured field block. The card also carries "Division" =
			// "Emory Univ Hosp-Midtown", which is an operating entity rather than
			// a department, so "Job Category" has to win.
			name:    "emory",
			host:    "clinical-emory.icims.com",
			fixture: "icims_emory_search_page1.html",
			count:   20,
			first: internal.JobPosting{
				Company:        "emory",
				URL:            "https://clinical-emory.icims.com/jobs/170893/medical-assistant/job",
				Title:          "Medical Assistant",
				Location:       "Johns Creek, GA, 30097",
				Department:     "Clinical & Nursing Support",
				EmploymentType: internal.EmploymentTypeFullTime,
				RequisitionID:  "170893",
				ExternalID:     "170893",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			postings := icimsBoard(t, tc.host, tc.fixture)

			must.SliceLen(t, tc.count, postings)

			want := tc.first
			want.Source = internal.PostingSource{Platform: icimsPlatform, Key: tc.host}

			test.Eq(t, &want, postings[0])

			for _, posting := range postings {
				test.StrHasPrefix(t, "https://"+tc.host+"/jobs/", posting.URL,
					test.Sprint("every posting URL must be on the tenant's own host"))
				test.StrNotContains(t, posting.URL, "in_iframe",
					test.Sprint("the embed flag this crawler adds must not reach a published URL"))
				test.StrNotEqFold(t, "", posting.Title)
				test.StrNotEqFold(t, "", posting.Location)
				test.StrNotEqFold(t, "", posting.ExternalID)
			}
		})
	}
}

// TestICIMSPublishesNoCompensation pins the decision documented above
// [icimsPostedLabels].
//
// careers-gdms publishes "Combined Salary Range" on all 20 cards of the captured
// page, so this is not passing for want of a pay field: it is asserting that a
// field this adapter deliberately declines to interpret stays uninterpreted. If
// someone later teaches it to read pay, this test failing is the prompt to write
// down which of the five measured formats they handled.
func TestICIMSPublishesNoCompensation(t *testing.T) {
	t.Parallel()

	postings := icimsBoard(t, "careers-gdms.icims.com", "icims_gdms_search_page1.html")

	must.SliceNotEmpty(t, postings)

	for _, posting := range postings {
		test.Nil(t, posting.Compensation)
	}
}

// TestICIMSFollowsRelNextToTheEnd asserts the walk uses the board's own next
// link and stops when it is gone.
func TestICIMSFollowsRelNextToTheEnd(t *testing.T) {
	t.Parallel()

	const host = "careers-gdms.icims.com"

	transport := &icimsPageTransport{pages: map[string]string{
		icimsSearchURL(host, 0): icimsFixture(t, "icims_gdms_search_page1.html"),
		icimsSearchURL(host, 1): icimsSinglePage(t, "icims_matchretail_search_page1.html"),
	}}

	postings, errs := drain(ICIMS(t.Context(), &http.Client{Transport: transport}, host))

	must.SliceEmpty(t, errs)

	// The second page is another tenant's capture, so its postings are on
	// careers-wow.icims.com and every one of them fails the same-host check.
	// That is the point: it proves the anchor host is checked per posting rather
	// than per board, and it is why the count is the first page's 20 rather than
	// 39.
	must.SliceLen(t, 20, postings)

	test.Eq(t, []string{
		"https://careers-gdms.icims.com/jobs/search?pr=0&in_iframe=1",
		"https://careers-gdms.icims.com/jobs/search?pr=1&in_iframe=1",
	}, transport.requests)
}

// icimsLoopTransport answers every request with the same page, which is what a
// board that ignores its pr parameter does.
type icimsLoopTransport struct {
	body     string
	requests int
}

func (tr *icimsLoopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.requests++

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(tr.body)),
		Request:    req,
	}, nil
}

// TestICIMSStopsOnABoardThatIgnoresItsPageParameter is the regression test for
// the failure this codebase has already paid for twice.
//
// A board answering every offset with the same page made three adapters issue
// 5,001 requests and yield 500,001 duplicate postings in under a second. The
// capture used here advertises rel=next on every response, so the board's own
// termination signal never fires and only [pageRepeatGuard] can end the walk.
func TestICIMSStopsOnABoardThatIgnoresItsPageParameter(t *testing.T) {
	t.Parallel()

	transport := &icimsLoopTransport{body: icimsFixture(t, "icims_gdms_search_page1.html")}

	postings, errs := drain(ICIMS(t.Context(), &http.Client{Transport: transport}, "careers-gdms.icims.com"))

	must.SliceEmpty(t, errs)

	test.Eq(t, 2, transport.requests,
		test.Sprint("a board repeating one page must end on the second request, not at icimsMaxPages"))

	test.SliceLen(t, 20, postings,
		test.Sprint("the repeated page must contribute its postings once, not twice"))
}

// TestICIMSNeverYieldsTheSamePostingTwice covers the drift measured on
// jobs-noodles, which returned 921 cards across 19 pages holding 893 distinct
// URLs because the board reorders between requests.
//
// [pageRepeatGuard] cannot catch that: the pages differ, they merely overlap.
// The two fixtures here are the same capture under two page numbers with the
// second one's next link removed, which is an overlap of 100%.
func TestICIMSNeverYieldsTheSamePostingTwice(t *testing.T) {
	t.Parallel()

	const host = "careers-wow.icims.com"

	page0 := icimsFixture(t, "icims_matchretail_search_page1.html")

	// The capture is a one-page board, so a next link is added to make it
	// advertise a second page carrying the same postings.
	page0 = strings.Replace(page0, "<head>", `<head><link rel="next" href="https://careers-wow.icims.com/jobs/search?pr=1&amp;in_iframe=1" />`, 1)

	transport := &icimsPageTransport{pages: map[string]string{
		icimsSearchURL(host, 0): page0,
		icimsSearchURL(host, 1): icimsFixture(t, "icims_matchretail_search_page1.html"),
	}}

	postings, errs := drain(ICIMS(t.Context(), &http.Client{Transport: transport}, host))

	must.SliceEmpty(t, errs)
	must.SliceLen(t, 19, postings, must.Sprint("an overlapping second page must not duplicate postings"))

	seen := map[string]bool{}
	for _, posting := range postings {
		test.False(t, seen[posting.URL], test.Sprintf("%s was yielded twice", posting.URL))
		seen[posting.URL] = true
	}
}

// TestICIMSReportsAPageOfCardsThatYieldedNothing covers the failure this project
// cares about most: a source that quietly reports zero.
func TestICIMSReportsAPageOfCardsThatYieldedNothing(t *testing.T) {
	t.Parallel()

	// The capture is served under a host that is not the one its anchors point
	// at, which is what a template change or a rehomed board looks like: cards
	// parse, and not one of them carries a URL on this tenant.
	transport := &icimsPageTransport{pages: map[string]string{
		icimsSearchURL("careers-elsewhere.icims.com", 0): icimsSinglePage(t, "icims_gdms_search_page1.html"),
	}}

	postings, errs := drain(ICIMS(t.Context(), &http.Client{Transport: transport}, "careers-elsewhere.icims.com"))

	must.SliceEmpty(t, postings)
	must.SliceLen(t, 1, errs)

	test.StrContains(t, errs[0].Error(), "20 job cards parsed but none carried a posting URL")
}

// TestICIMSCanonicalURL pins the two checks that keep a posting URL on the
// tenant's own host and free of this crawler's own embed flag.
func TestICIMSCanonicalURL(t *testing.T) {
	t.Parallel()

	const host = "careers-gdms.icims.com"

	cases := []struct {
		name string
		href string
		want string
	}{
		{
			name: "drops the embed flag",
			href: "https://careers-gdms.icims.com/jobs/73835/engineer/job?in_iframe=1",
			want: "https://careers-gdms.icims.com/jobs/73835/engineer/job",
		},
		{
			name: "drops a fragment",
			href: "https://careers-gdms.icims.com/jobs/73835/engineer/job#apply",
			want: "https://careers-gdms.icims.com/jobs/73835/engineer/job",
		},
		{
			name: "rejects another tenant on the same platform",
			href: "https://careers-peraton.icims.com/jobs/73835/engineer/job",
		},
		{
			name: "rejects an apply URL on another ATS",
			href: "https://boards.greenhouse.io/gdms/jobs/73835",
		},
		{
			name: "rejects a lookalike host",
			href: "https://careers-gdms.icims.com.example.net/jobs/73835/engineer/job",
		},
		{
			name: "rejects a non-posting path",
			href: "https://careers-gdms.icims.com/jobs/search?pr=1",
		},
		{
			name: "rejects a non-numeric id",
			href: "https://careers-gdms.icims.com/jobs/search-results/engineer/job",
		},
		{
			name: "rejects plain http",
			href: "http://careers-gdms.icims.com/jobs/73835/engineer/job",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := icimsCanonicalURL(host, tc.href)

			test.Eq(t, tc.want != "", ok)
			test.Eq(t, tc.want, got)
		})
	}
}

// TestICIMSCompanyName covers the host shapes in the candidate file, not only
// the ones registered.
func TestICIMSCompanyName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"careers-petsuppliesplus.icims.com":     "petsuppliesplus",
		"career-schwab.icims.com":               "schwab",
		"jobs-express.icims.com":                "express",
		"clinical-emory.icims.com":              "emory",
		"storecareers-gpminvestments.icims.com": "gpminvestments",

		// Employer names that contain a hyphen. Stripping past the first segment
		// would eat half of each.
		"careers-gd-ots.icims.com":          "gd-ots",
		"careers-atlas-aerospace.icims.com": "atlas-aerospace",

		// Three-segment audience prefixes, which are why the second strip exists.
		// Both are in the candidate file and neither is registered.
		"internal-careers-rivian.icims.com":   "rivian",
		"manufacturing-jobs-marvin.icims.com": "marvin",
	}

	for host, want := range cases {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, want, icimsCompanyName(host))
		})
	}
}

// TestICIMSDateOrderIsInferredFromTheWholeBoard covers the ambiguity that a
// single card cannot settle.
func TestICIMSDateOrderIsInferredFromTheWholeBoard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		values []string
		want   icimsDateOrder
	}{
		{
			name:   "a day above twelve settles day-first",
			values: []string{"3/7/2026 08:17", "23/07/2026 08:17"},
			want:   icimsDateOrderDayFirst,
		},
		{
			name:   "a month above twelve settles month-first",
			values: []string{"3/7/2026 2:40 PM", "7/28/2026 2:40 PM"},
			want:   icimsDateOrderMonthFirst,
		},
		{
			name:   "a twelve-hour clock alone settles month-first",
			values: []string{"3/7/2026 2:40 PM"},
			want:   icimsDateOrderMonthFirst,
		},
		{
			name:   "nothing but ambiguous dates stays unknown",
			values: []string{"3/7/2026 08:17", "1/2/2026 08:17"},
			want:   icimsDateOrderUnknown,
		},
		{
			name:   "contradictory evidence stays unknown",
			values: []string{"23/07/2026 08:17", "7/28/2026 2:40 PM"},
			want:   icimsDateOrderUnknown,
		},
		{
			name:   "no dates at all stays unknown",
			values: nil,
			want:   icimsDateOrderUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var evidence icimsDateEvidence

			for _, value := range tc.values {
				evidence.observe(value)
			}

			test.Eq(t, int(tc.want), int(evidence.order()))
		})
	}
}

// TestICIMSTimeRefusesToGuess asserts that an unresolved date order publishes no
// date rather than a plausible wrong one.
func TestICIMSTimeRefusesToGuess(t *testing.T) {
	t.Parallel()

	test.Eq(t, time.Time{}, icimsTime("3/7/2026 08:17", icimsDateOrderUnknown))

	test.Eq(t,
		time.Date(2026, time.March, 7, 8, 17, 0, 0, time.UTC),
		icimsTime("3/7/2026 08:17", icimsDateOrderMonthFirst))

	test.Eq(t,
		time.Date(2026, time.July, 3, 8, 17, 0, 0, time.UTC),
		icimsTime("3/7/2026 08:17", icimsDateOrderDayFirst))

	// A date with no clock, which is the shape of career-schwab's "Application
	// deadline".
	test.Eq(t,
		time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC),
		icimsTime("8/7/2026", icimsDateOrderMonthFirst))

	test.Eq(t, time.Time{}, icimsTime("23 minutes ago", icimsDateOrderMonthFirst))
	test.Eq(t, time.Time{}, icimsTime("", icimsDateOrderMonthFirst))
}

// TestICIMSNextPageMustMoveForward stops a board from pinning the walk on one
// page by advertising itself, or an earlier page, as next.
func TestICIMSNextPageMustMoveForward(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		head string
		page int
		want int
		ok   bool
	}{
		{
			name: "forward",
			head: `<link rel="next" href="https://x.icims.com/jobs/search?pr=4&amp;in_iframe=1" />`,
			page: 3,
			want: 4,
			ok:   true,
		},
		{
			name: "same page",
			head: `<link rel="next" href="https://x.icims.com/jobs/search?pr=3&amp;in_iframe=1" />`,
			page: 3,
		},
		{
			name: "backwards",
			head: `<link rel="next" href="https://x.icims.com/jobs/search?pr=1&amp;in_iframe=1" />`,
			page: 3,
		},
		{
			name: "absent",
			head: `<link rel="canonical" href="https://x.icims.com/jobs/search?pr=3" />`,
			page: 3,
		},
		{
			name: "no page parameter",
			head: `<link rel="next" href="https://x.icims.com/jobs/search" />`,
			page: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, err := html.Parse(strings.NewReader("<html><head>" + tc.head + "</head><body></body></html>"))
			must.NoError(t, err)

			next, ok := icimsNextPage(doc, tc.page)

			test.Eq(t, tc.ok, ok)
			test.Eq(t, tc.want, next)
		})
	}
}

// TestICIMSRegisteredHostsAreStagedCandidates holds this platform to the rule the
// rest of the registry follows: a registered tenant must appear in its
// platform's candidate file, so the staged list stays the whole researched
// universe rather than the leftovers.
func TestICIMSRegisteredHostsAreStagedCandidates(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "icims_hosts.txt")

	must.Greater(t, 1_000, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	for _, host := range ICIMSHosts {
		test.True(t, candidates[host],
			test.Sprintf("registered host %q is not in testdata/candidates/icims_hosts.txt", host))
	}

	test.Less(t, len(candidates), len(ICIMSHosts), test.Sprint("the registered list should stay a subset of the candidates"))
}

// TestICIMSHostsAreWellFormedAndUnique guards the registry itself.
func TestICIMSHostsAreWellFormedAndUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for _, host := range ICIMSHosts {
		test.StrHasSuffix(t, ".icims.com", host)
		test.Eq(t, strings.ToLower(host), host, test.Sprint("hosts are limiter keys and are compared lowercased"))
		test.False(t, seen[host], test.Sprintf("%s is registered twice", host))
		test.StrNotEqFold(t, "", icimsCompanyName(host), test.Sprintf("%s reduces to an empty company name", host))

		seen[host] = true
	}

	test.True(t, slices.IsSorted(ICIMSHosts), test.Sprint("keep the list sorted so a diff shows what changed"))
}

// TestICIMSSharesOnePacingKey asserts the registration in this file's init
// actually took, because the cost of it not having is invisible until a crawl
// is rate-limited.
//
// Without it each of the 70 hosts gets its own four-slot limiter and this
// platform alone can put 280 concurrent requests on one vendor backend, which is
// the shape that rate-limited 56 Workable boards into looking dead.
func TestICIMSSharesOnePacingKey(t *testing.T) {
	t.Parallel()

	must.SliceNotEmpty(t, ICIMSHosts)

	first := httpx.ServicePolicyForHost(ICIMSHosts[0], httpx.DefaultPerHostLimit)

	test.Eq(t, icimsPlatform, first.Key)
	test.NotEq(t, time.Duration(0), first.Interval, test.Sprint("a shared backend must be paced"))

	for _, host := range ICIMSHosts {
		test.Eq(t, first.Key, httpx.ServicePolicyForHost(host, httpx.DefaultPerHostLimit).Key,
			test.Sprintf("%s must share the platform limiter key", host))
	}
}

// TestICIMSLive walks every registered board against the live platform.
func TestICIMSLive(t *testing.T) {
	t.Parallel()

	testMultipleParallel(t, slices.Values(ICIMSHosts), ICIMS)
}

// TestICIMSLiveSingle is the smallest live check: one board, walked to the end.
func TestICIMSLiveSingle(t *testing.T) {
	t.Parallel()

	testSingle(t, "careers-wow.icims.com", ICIMS)
}
