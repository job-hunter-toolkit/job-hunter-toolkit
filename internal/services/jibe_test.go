package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestJibe(t *testing.T) {
	testSingle(t, "bjc", Jibe)
}

func TestJibe_all(t *testing.T) {
	testMultipleParallel(t, slices.Values(JibeCompanies), Jibe)
}

// jibeFullPage builds a response holding exactly one full page of postings, so
// the short-page check cannot be what ends a pagination loop under test.
func jibeFullPage(prefix string, totalCount int) string {
	jobs := make([]string, jibePageSize)

	for i := range jobs {
		jobs[i] = fmt.Sprintf(`{"data":{"title":"Job %s%d","apply_url":"https://acme.jibeapply.com/jobs/%s%d","full_location":"Chicago, IL"}}`, prefix, i, prefix, i)
	}

	return `{"jobs":[` + strings.Join(jobs, ",") + `],"totalCount":` + strconv.Itoa(totalCount) + `,"meta_data":false}`
}

// TestJibeStopsWhenTheBoardIgnoresPage is a regression test.
//
// The loop used to end only on a short page, so a tenant that answers every
// "page" with the same full page was crawled until the crawl deadline: measured
// at 5,001 requests and 500,001 duplicate postings against a stub like this one
// in 0.8 seconds.
//
// totalCount here is far larger than what the stub serves, so the repeated-page
// check is the only thing that can end this crawl.
func TestJibeStopsWhenTheBoardIgnoresPage(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(jibeFullPage("", 500_000))

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, jibePageSize, postings)
}

// TestJibeRecordsItsSourceIdentity covers the one enrichment this adapter got.
//
// The rest of Jibe's payload stays unmodelled on purpose: no live body has ever
// been decoded here beyond the four fields the adapter reads, and a guessed
// field *type* fails the decode and takes the tenant with it — which is exactly
// what modelling "meta_data" as a struct did to nine large employers. Source is
// different in kind: it comes from the registration rather than the response, so
// it needs no guess about what the board sends.
func TestJibeRecordsItsSourceIdentity(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"jibeapply.com": `{
			"jobs": [{"data": {
				"title": "Cloud Security Engineer",
				"apply_url": "https://acme.jibeapply.com/jobs/1",
				"full_location": "Chicago, IL"
			}}],
			"totalCount": 1,
			"meta_data": false
		}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, internal.PostingSource{Platform: jibePlatform, Key: "acme"}, postings[0].Source)
}

// TestJibeStopsAtItsReportedTotal is a regression test.
//
// jibeJobs has decoded "totalCount" since the adapter was written and never read
// it, so a tenant whose posting count is an exact multiple of the page size was
// always asked for one page more than exists.
func TestJibeStopsAtItsReportedTotal(t *testing.T) {
	t.Parallel()

	// Two full, distinct pages: the repeated-page check cannot fire and neither
	// page is short, so only the total can stop this.
	client, transport := fixtureClient(map[string]string{
		"page=1": jibeFullPage("a", 2*jibePageSize),
		"page=2": jibeFullPage("b", 2*jibePageSize),
	})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	// Page three has no fixture, so an adapter that still ignores the total gets
	// a 404 and reports it here.
	must.SliceEmpty(t, errs)

	test.Len(t, 2, transport.requests)
	test.Len(t, 2*jibePageSize, postings)
}

// TestJibeIgnoresATotalOfOnePage pins down the one reading of totalCount that is
// deliberately not trusted.
//
// A totalCount equal to the page size is indistinguishable from a per-page
// count, and treating a per-page count as a grand total would cap every large
// tenant at 100 postings, a silently truncated source; paying one extra request
// is by far the cheaper mistake.
func TestJibeIgnoresATotalOfOnePage(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"page=1": jibeFullPage("a", jibePageSize),
		"page=2": `{"jobs":[{"data":{"title":"Last Job","apply_url":"https://acme.jibeapply.com/jobs/last","full_location":"Chicago, IL"}}],"totalCount":` + strconv.Itoa(jibePageSize) + `,"meta_data":false}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	test.Len(t, 2, transport.requests)
	test.Len(t, jibePageSize+1, postings)
}

// TestJibeStopsWhenTheConsumerDoes guards the iterator contract the health
// command depends on: it caps each source at 100 postings by returning false
// from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
func TestJibeStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(jibeFullPage("", 500_000))

	var seen int

	for range Jibe(t.Context(), client, "acme") {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, transport.requests)
}

// TestJibeHostAcceptsBothKeyForms is a regression test.
//
// This adapter only ever built "{key}.jibeapply.com", but iCIMS serves the
// identical /api/jobs endpoint from employers' own domains, so every board on a
// vanity host was invisible to the crawl despite the response shape already
// being modelled here. The .icims.com host is not an alternative: it 404s on
// /api/jobs.
func TestJibeHostAcceptsBothKeyForms(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  string
		host string
		name string
	}{
		{key: "fedex", host: "fedex.jibeapply.com", name: "fedex"},
		{key: "careers.costco.com", host: "careers.costco.com", name: "costco"},
		{key: "jobs.jcp.com", host: "jobs.jcp.com", name: "jcp"},
		{key: "careers.se.com", host: "careers.se.com", name: "se"},
		{key: "www.cakecareers.com", host: "www.cakecareers.com", name: "cakecareers"},
		{key: "aus.jibeapply.com", host: "aus.jibeapply.com", name: "aus"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			test.Eq(t, tc.host, jibeHost(tc.key))
			test.Eq(t, tc.name, jibeCompanyName(tc.key))
		})
	}
}

// TestJibeRequestsTheVanityHost proves the key reaches the wire, since the
// mapping above is only worth anything if the request actually goes there.
func TestJibeRequestsTheVanityHost(t *testing.T) {
	t.Parallel()

	var requested string

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requested = req.URL.Host

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"totalCount":0,"jobs":[]}`)),
			Request:    req,
		}, nil
	})}

	drain(Jibe(t.Context(), client, "careers.costco.com"))

	test.Eq(t, "careers.costco.com", requested)
}

// TestJibeVanityHostsAreNotDoubleRegistered guards the company list itself: a
// vanity host whose derived name duplicates an existing bare slug would crawl
// the same employer twice and report it twice in the company list.
func TestJibeVanityHostsAreNotDoubleRegistered(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}

	for _, key := range JibeCompanies {
		name := jibeCompanyName(key)

		if previous, ok := seen[name]; ok {
			t.Errorf("keys %q and %q both resolve to company %q", previous, key, name)
		}

		seen[name] = key
	}
}

// jibePageTransport serves one canned body per page number.
//
// It exists rather than reusing [fixtureTransport] because the vanity-host path
// fetches pages concurrently, and fixtureTransport appends to an unsynchronised
// slice. It also records the highest number of requests that were ever in flight
// at once, which is the only way to tell the fan-out apart from the sequential
// loop from outside the adapter.
type jibePageTransport struct {
	mu sync.Mutex

	// pages maps a page number to the body served for it. A page with no entry
	// gets an empty result rather than a 404, which is what a real board does
	// past its last page.
	pages map[int]string

	// hold is how long each response is delayed. Without it a fan-out can finish
	// each request before the next one starts and look sequential.
	hold time.Duration

	requests    int
	inFlight    int
	maxInFlight int
	pagesSeen   []int
}

func (j *jibePageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))

	j.mu.Lock()
	j.requests++
	j.inFlight++
	j.maxInFlight = max(j.maxInFlight, j.inFlight)
	j.pagesSeen = append(j.pagesSeen, page)
	body, ok := j.pages[page]
	j.mu.Unlock()

	time.Sleep(j.hold)

	j.mu.Lock()
	j.inFlight--
	j.mu.Unlock()

	if !ok {
		body = `{"jobs":[],"totalCount":0}`
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func jibePageClient(pages map[int]string) (*http.Client, *jibePageTransport) {
	transport := &jibePageTransport{pages: pages, hold: 20 * time.Millisecond}

	return &http.Client{Transport: transport}, transport
}

// jibeFixture reads a response captured from a live Jibe board.
func jibeFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// TestJibeParsesACapturedVanityHost is the fixture that decides whether this
// adapter reads Jibe, as opposed to reading the shape a document said Jibe has.
// The body is https://jobs.jcp.com/api/jobs?limit=100&page=1 as captured on
// 2026-07-28, cut to its first two postings with each posting's "description"
// removed; every other key, and every value, is JCPenney's own.
//
// What the capture establishes, and what a hand-written fixture could not:
//
//   - the vanity host is real. docs/research/ats-platform-survey.md marks the
//     ~261 employer-owned hostnames "verified-in-code", meaning read out of a
//     slug list, never fetched. jobs.jcp.com answers, and it answers with the
//     shape jibeJobs already modelled.
//   - posted_date is NOT RFC 3339. It arrives as "2026-07-27T23:39:00+0000",
//     with no colon in the zone offset, so [time.RFC3339] rejects it. All 906
//     timestamps sampled across four boards had that shape, so an adapter that
//     had reached for the obvious layout would have published a posting date on
//     none of the platform while looking healthy.
//   - employment_type is the schema.org enum, present on 84.5% of the 29,230
//     live postings measured, and "FULL_TIME" normalizes cleanly.
//   - the top-level "meta_data" really is the bare `false` this file has warned
//     about since it broke nine employers, while the per-posting data.meta_data
//     is an object in the same response. Both are in this capture untouched.
//   - "department" is present and empty on this board. It is populated on about
//     1% of the platform, which is why data.categories — 96.7% present — was
//     considered for the field and rejected: its live values include job titles.
func TestJibeParsesACapturedVanityHost(t *testing.T) {
	t.Parallel()

	client, _ := jibePageClient(map[int]string{1: jibeFixture(t, "jibe_jcp_jobs.json")})

	postings, errs := drain(Jibe(t.Context(), client, "jobs.jcp.com"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	manager := postings[0]

	test.Eq(t, "jcp", manager.Company)
	test.Eq(t, "Store Manager", manager.Title)
	test.Eq(t, "Thornton, Colorado", manager.Location)
	test.Eq(t, "https://careers-jcpenney.icims.com/jobs/9291/login", manager.URL)
	test.Eq(t, internal.EmploymentTypeFullTime, manager.EmploymentType)
	test.Eq(t, "9291", manager.RequisitionID)
	test.Eq(t, "9291", manager.ExternalID)
	test.Eq(t, "", manager.Department)
	test.Nil(t, manager.Compensation)
	test.Eq(t, internal.PostingSource{Platform: jibePlatform, Key: "jobs.jcp.com"}, manager.Source)

	// "2026-07-27T23:39:00+0000" and "2026-07-28T00:41:39+0000", in UTC.
	test.Eq(t, time.Date(2026, time.July, 27, 23, 39, 0, 0, time.UTC), manager.PostedAt)
	test.Eq(t, time.Date(2026, time.July, 28, 0, 41, 39, 0, time.UTC), manager.UpdatedAt)
}

// TestJibeParsesCapturedPay is the second half of the live capture, from a board
// that publishes pay. The body is
// https://petsmart.jibeapply.com/api/jobs?limit=100&page=1 as captured on
// 2026-07-28, cut to three postings that carry a salary, descriptions removed.
//
// It pins two corrections to this file's own comments:
//
//   - salary_currency is the empty string, not a code. It was populated on 43 of
//     the 205 postings that show pay across 333 boards, and on none of the 74
//     PetSmart postings that produced the original "populates them often"
//     measurement. The adapter must still emit the range: the amounts and the
//     period are real, and dropping a $13.66/hr range because the board omitted
//     "USD" would discard the only employer-published pay on the platform.
//   - pay here is hourly and small. [internal.Compensation]'s magnitude fallback
//     would have guessed that correctly, but salary_frequency says so outright
//     on every PetSmart posting, and a board quoting 4,500 monthly would defeat
//     the guess.
func TestJibeParsesCapturedPay(t *testing.T) {
	t.Parallel()

	client, _ := jibePageClient(map[int]string{1: jibeFixture(t, "jibe_petsmart_jobs.json")})

	postings, errs := drain(Jibe(t.Context(), client, "petsmart"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	lead := postings[0]

	test.Eq(t, "petsmart", lead.Company)
	test.Eq(t, "Retail Operations Lead", lead.Title)
	test.Eq(t, "Bayamon, Puerto Rico", lead.Location)

	must.NotNil(t, lead.Compensation)
	test.Eq(t, 13.66, lead.Compensation.Min)
	test.Eq(t, 19.16, lead.Compensation.Max)
	test.Eq(t, "", lead.Compensation.Currency)
	test.Eq(t, internal.PeriodHour, lead.Compensation.Period)
	test.Eq(t, internal.ProvenanceEmployer, lead.Compensation.Provenance)

	// This board publishes no employment type at all, on any of its 10,847
	// postings. An absent field must stay absent rather than become full-time.
	test.Eq(t, internal.EmploymentTypeUnknown, lead.EmploymentType)

	test.Eq(t, "104479036723-41447668006", lead.RequisitionID)
	test.Eq(t, time.Date(2025, time.January, 23, 0, 0, 0, 0, time.UTC), lead.PostedAt)
}

// TestJibeFansOutAcrossPagesOnAVanityHost is the throughput half of the vanity
// -host work.
//
// An employer's own careers host is host-isolated: httpx.servicePolicyFor gives
// it its own limiter key rather than folding it into the shared jibeapply.com
// bucket, so page requests for one board do not contend with the rest of the
// platform. totalCount has been decoded since this adapter was written and, once
// the first page proves the board honours a 100-posting window, it says exactly
// how many pages exist — which turns a strictly sequential walk into a bounded
// fan-out. It is not a micro-optimisation at this scale:
// careers.dollargeneral.com reported 88,854 postings on 2026-07-28, which is 889
// sequential round trips today.
func TestJibeFansOutAcrossPagesOnAVanityHost(t *testing.T) {
	t.Parallel()

	client, transport := jibePageClient(map[int]string{
		1: jibeFullPage("a", 350),
		2: jibeFullPage("b", 350),
		3: jibeFullPage("c", 350),
		4: `{"jobs":[{"data":{"title":"Last Job","apply_url":"https://careers.example.com/jobs/last","full_location":"Chicago, IL"}}],"totalCount":350,"meta_data":false}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "careers.example.com"))

	must.SliceEmpty(t, errs)

	// Every page the reported total implies, and not one more: 350 postings is
	// four pages of 100, so page five is never asked for.
	test.Eq(t, 4, transport.requests)
	test.Len(t, 3*jibePageSize+1, postings)
	test.Greater(t, 1, transport.maxInFlight, test.Sprint("pages after the first should be fetched concurrently"))
}

// TestJibeDoesNotFanOutOnTheSharedBackend is the other half of that decision,
// and the more important one.
//
// Every *.jibeapply.com tenant is collapsed onto one "jibeapply.com" limiter key
// by httpx.servicePolicyFor, so concurrent page requests there cannot go any
// faster: they only park more goroutines on a semaphore the whole platform
// shares, while looking like a speedup in a diff. A key containing a dot is not
// sufficient evidence of isolation either — "aus.jibeapply.com" is a registered
// board on the shared host.
func TestJibeDoesNotFanOutOnTheSharedBackend(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"acme", "aus.jibeapply.com"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			client, transport := jibePageClient(map[int]string{
				1: jibeFullPage("a", 350),
				2: jibeFullPage("b", 350),
				3: jibeFullPage("c", 350),
				4: `{"jobs":[{"data":{"title":"Last Job","apply_url":"https://acme.jibeapply.com/jobs/last","full_location":"Chicago, IL"}}],"totalCount":350,"meta_data":false}`,
			})

			postings, errs := drain(Jibe(t.Context(), client, key))

			must.SliceEmpty(t, errs)
			test.Len(t, 3*jibePageSize+1, postings)
			test.Eq(t, 1, transport.maxInFlight, test.Sprint("the shared backend must be paged one request at a time"))
		})
	}
}

// TestJibeFanOutStopsWhenTheBoardIgnoresPage is the fan-out's version of the
// regression [TestJibeStopsWhenTheBoardIgnoresPage] pins for the sequential
// loop.
//
// A board that answers every page with page one cannot be caught by comparing
// consecutive pages when they arrive out of order, so the guard has to be a set
// of page fingerprints. Without it, a board reporting a large total would have
// its first page yielded once per scheduled page.
func TestJibeFanOutStopsWhenTheBoardIgnoresPage(t *testing.T) {
	t.Parallel()

	same := jibeFullPage("a", 1000)

	client, _ := jibePageClient(map[int]string{
		1: same, 2: same, 3: same, 4: same, 5: same,
		6: same, 7: same, 8: same, 9: same, 10: same,
	})

	postings, errs := drain(Jibe(t.Context(), client, "careers.example.com"))

	must.SliceEmpty(t, errs)

	// The first page is yielded once. Every later page is recognised as a repeat
	// of it and contributes nothing, however many were scheduled.
	test.Len(t, jibePageSize, postings)
}

// TestJibeShortFirstPageEndsTheBoard pins the rule that keeps a board's own
// pages ahead of its own total.
//
// totalCount counts what the search matched, and boards report figures larger
// than the pages they will serve. Fanning out on the total alone would ask a
// board that has already answered with 3 postings for 61 more pages.
func TestJibeShortFirstPageEndsTheBoard(t *testing.T) {
	t.Parallel()

	client, transport := jibePageClient(map[int]string{
		1: `{"jobs":[{"data":{"title":"Only Job","apply_url":"https://careers.example.com/jobs/1","full_location":"Chicago, IL"}}],"totalCount":6172,"meta_data":false}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "careers.example.com"))

	must.SliceEmpty(t, errs)
	test.Len(t, 1, postings)
	test.Eq(t, 1, transport.requests)
}

// TestJibePagesAfter covers the page plan directly, including the reading of
// totalCount that is deliberately not trusted.
func TestJibePagesAfter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		total int
		first int
		pages []int
		known bool
	}{
		{name: "a total of one page is a per-page count and is not trusted", total: jibePageSize, first: jibePageSize},
		{name: "no total at all", total: 0, first: jibePageSize},
		{name: "two pages", total: 150, first: jibePageSize, pages: []int{2}, known: true},
		{name: "an exact multiple", total: 300, first: jibePageSize, pages: []int{2, 3}, known: true},
		{name: "the total is already served", total: 120, first: 120, known: true},
		{name: "a runaway total is capped", total: 100_000_000, first: jibePageSize, known: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan := jibePagesAfter(tc.total, tc.first)

			test.Eq(t, tc.known, plan.known)

			if tc.name == "a runaway total is capped" {
				test.Len(t, jibeMaxPages-1, plan.pages)

				return
			}

			test.Eq(t, tc.pages, plan.pages)
		})
	}
}

// TestJibeScalarAbsorbsBothSpellings guards the decode against the failure this
// file has warned about longest.
//
// Modelling the polymorphic "meta_data" as a struct broke nine large employers
// at once, because a wrong Go *type* fails the whole decode and takes the tenant
// with it. Every field this adapter now reads is a string on all 333 boards
// measured on 2026-07-28, but nothing in the API promises that, and a single
// tenant quoting req_id as a number would otherwise cost that employer entirely.
func TestJibeScalarAbsorbsBothSpellings(t *testing.T) {
	t.Parallel()

	client, _ := jibePageClient(map[int]string{1: `{
		"jobs": [{"data": {
			"title": "Cloud Security Engineer",
			"apply_url": "https://acme.jibeapply.com/jobs/1",
			"full_location": "Chicago, IL",
			"req_id": 90210,
			"slug": null,
			"employment_type": "PART_TIME",
			"department": {"name": "an object is not a department"},
			"salary_min_value": "42000",
			"salary_max_value": 51000,
			"salary_frequency": "YEARLY"
		}}],
		"totalCount": 1,
		"meta_data": false
	}`})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	posting := postings[0]

	test.Eq(t, "90210", posting.RequisitionID)
	test.Eq(t, "", posting.ExternalID)
	test.Eq(t, "", posting.Department)
	test.Eq(t, internal.EmploymentTypePartTime, posting.EmploymentType)

	must.NotNil(t, posting.Compensation)
	test.Eq(t, 42000.0, posting.Compensation.Min)
	test.Eq(t, 51000.0, posting.Compensation.Max)
	test.Eq(t, internal.PeriodYear, posting.Compensation.Period)
}

// candidateHosts reads the staged vanity-host list, dropping comments and blanks.
func candidateHosts(t *testing.T, name string) map[string]bool {
	t.Helper()

	file, err := os.Open(filepath.Join("testdata", "candidates", name))
	must.NoError(t, err)

	defer file.Close()

	entries := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")

		if line = strings.TrimSpace(line); line != "" {
			entries[line] = true
		}
	}

	must.NoError(t, scanner.Err())

	return entries
}

// TestJibeRegistersOnlyStagedHosts keeps the registry and the candidate file in
// step, the way every other platform's candidate list is kept.
//
// The file is where a host's evidence lives: what it answered with, when, and
// which board it duplicates. A registered host that is not in it is a host whose
// evidence nobody can find, which is the state this project's coverage decayed
// through once already. Bare slugs predate the file and are exempt.
func TestJibeRegistersOnlyStagedHosts(t *testing.T) {
	t.Parallel()

	staged := candidateHosts(t, "jibe_vanity_hosts.txt")

	must.Greater(t, 100, len(staged), must.Sprint("the candidate file should hold the full probed list"))

	var registeredHosts int

	for _, key := range JibeCompanies {
		if !strings.Contains(key, ".") {
			continue
		}

		registeredHosts++

		test.True(t, staged[key], test.Sprintf("registered host %q is not in testdata/candidates/jibe_vanity_hosts.txt", key))
	}

	test.Less(t, len(staged), registeredHosts, test.Sprint("the registered hosts should stay a subset of the staged ones"))
}

// TestJibeDoesNotYieldAfterTheConsumerStops is a regression test for a hazard
// the fan-out introduced.
//
// The page loops moved out of the iterator body into helpers, so "the consumer
// returned false" stopped being a return from the closure and became a return
// from a helper — after which the closure went on to its cancellation check and
// could call yield a second time. Yielding after a range-over-func consumer has
// returned false panics, and it takes the whole crawl worker with it.
//
// Both stop conditions are made true at once here: the consumer breaks on the
// first posting while the context that governs the crawl is already cancelled.
func TestJibeDoesNotYieldAfterTheConsumerStops(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"acme", "careers.example.com"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			client, _ := jibePageClient(map[int]string{
				1: jibeFullPage("a", 350),
				2: jibeFullPage("b", 350),
				3: jibeFullPage("c", 350),
			})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var seen int

			// The cancel happens while the iterator is mid-page, so the emit
			// path sees a cancelled context on its next posting.
			for range Jibe(ctx, client, key) {
				seen++

				cancel()

				break
			}

			test.Eq(t, 1, seen)
		})
	}
}
