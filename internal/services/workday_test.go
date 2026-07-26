package services

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestWorkday(t *testing.T) {
	testSingle(t, "https://comcast.wd5.myworkdayjobs.com/Comcast_Careers", Workday)
}

func TestWorkday_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	testMultipleParallel(t, slices.Values(WorkdayCompanyURLs), Workday)
}

// workdayTestTenant is the tenant URL used by the hermetic tests below. Its host
// is shaped like a real one so httpx keys its limiter the way it would in a
// crawl (one bucket per tenant host).
const workdayTestTenant = "https://acme.wd1.myworkdayjobs.com/AcmeCareers"

// workdayStallGuard bounds how long a hermetic Workday test may run.
//
// The bug these tests cover does not fail fast: it parks on httpx's per-service
// semaphore until the client's two-minute timeout expires. Without a guard the
// regression would show up as a test binary that appears to hang, which is far
// harder to read than an explicit failure.
const workdayStallGuard = 30 * time.Second

// workdayStubRequest is one recorded page request, decoded from the JSON body
// the adapter posts.
type workdayStubRequest struct {
	limit  int
	offset int
}

// workdayStub serves synthetic "cxs" pages for one fake tenant, and records
// enough about the exchange to assert on paging, body hygiene, and concurrency.
//
// It is deliberately not fixtureTransport: these tests need per-request state
// (offset-aware bodies, close accounting) and run the adapter concurrently, so
// the recording has to be mutex-guarded.
type workdayStub struct {
	// total is the posting count the tenant advertises in its "total" field.
	total int

	// served, when non-zero, is how many postings a page actually contains no
	// matter what limit was requested. Tenants are documented to clamp the page
	// size, and the adapter must page by what it received, not what it asked
	// for. Zero honours the requested limit.
	served int

	// serveUpTo, when non-zero, is how many postings the tenant will actually
	// hand out, regardless of the total it advertises. Boards with a stale count
	// behave this way, and the pages past the real end come back empty.
	serveUpTo int

	// ignoreOffset makes every request answer with the same page, which is what
	// a tenant that does not implement "offset" looks like from the outside.
	ignoreOffset bool

	// rejectLimitAbove, when non-zero, answers 400 to any request asking for a
	// larger page than this.
	rejectLimitAbove int

	// failFromOffset, when non-zero, answers 404 for offsets at or above it.
	failFromOffset int

	// delay is slept inside RoundTrip so overlapping requests are observable.
	delay time.Duration

	mu       sync.Mutex
	requests []workdayStubRequest
	opened   int
	closed   int
	openNow  int
	peakOpen int
	inFlight int
	peak     int
}

// workdayStubState is a consistent snapshot of what the stub observed.
type workdayStubState struct {
	requests []workdayStubRequest

	// opened and closed count response bodies handed out and closed.
	opened, closed int

	// peakOpen is the most bodies that were ever open at the same moment.
	//
	// This, not the opened/closed totals, is what catches the leak: the old
	// adapter closed every body with a `defer` inside its pagination loop, so
	// the totals still balanced once the iterator returned. What was wrong was
	// *when*: the bodies piled up, one per page, holding a httpx concurrency
	// slot each.
	peakOpen int

	// peak is the most requests that were ever in flight at the same moment.
	peak int
}

// workdayStubBody accounts for a body exactly once, so a body closed twice
// cannot mask another that was never closed at all.
type workdayStubBody struct {
	io.Reader

	once sync.Once
	stub *workdayStub
}

func (b *workdayStubBody) Close() error {
	b.once.Do(func() {
		b.stub.mu.Lock()
		defer b.stub.mu.Unlock()

		b.stub.closed++
		b.stub.openNow--
	})

	return nil
}

func (s *workdayStub) body(content string) io.ReadCloser {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.opened++
	s.openNow++
	s.peakOpen = max(s.peakOpen, s.openNow)

	return &workdayStubBody{Reader: strings.NewReader(content), stub: s}
}

func (s *workdayStub) respond(req *http.Request, status int, content string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       s.body(content),
		Request:    req,
	}
}

func (s *workdayStub) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("workday stub: reading request body: %w", err)
	}

	_ = req.Body.Close()

	var page workdayStubRequest

	var decoded struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}

	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("workday stub: malformed request body %q: %w", raw, err)
	}

	page.limit, page.offset = decoded.Limit, decoded.Offset

	s.mu.Lock()
	s.requests = append(s.requests, page)
	s.inFlight++
	s.peak = max(s.peak, s.inFlight)
	s.mu.Unlock()

	if s.delay > 0 {
		time.Sleep(s.delay)
	}

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.inFlight--
	}()

	switch {
	case s.rejectLimitAbove > 0 && page.limit > s.rejectLimitAbove:
		return s.respond(req, http.StatusBadRequest, `{"error":"limit too large"}`), nil
	case s.failFromOffset > 0 && page.offset >= s.failFromOffset:
		return s.respond(req, http.StatusNotFound, `{"error":"gone"}`), nil
	}

	offset := page.offset
	if s.ignoreOffset {
		offset = 0
	}

	size := min(cmp.Or(s.served, page.limit), page.limit)

	if !s.ignoreOffset {
		size = min(size, max(0, cmp.Or(s.serveUpTo, s.total)-offset))
	}

	postings := make([]string, 0, size)
	for i := range size {
		postings = append(postings, fmt.Sprintf(
			`{"title":"Job %[1]d","externalPath":"/job/%[1]d","locationsText":"Remote",`+
				`"postedOn":"Posted Today","bulletFields":["JR%[1]d","Full time"]}`,
			offset+i,
		))
	}

	return s.respond(req, http.StatusOK, fmt.Sprintf(
		`{"total":%d,"jobPostings":[%s]}`, s.total, strings.Join(postings, ","),
	)), nil
}

func (s *workdayStub) state() workdayStubState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return workdayStubState{
		requests: slices.Clone(s.requests),
		opened:   s.opened,
		closed:   s.closed,
		peakOpen: s.peakOpen,
		peak:     s.peak,
	}
}

// workdayCollect drains an adapter under a stall guard.
//
// A regression on the body leak does not return a wrong answer, it stops
// returning at all, so every hermetic test here has to be able to fail rather
// than hang.
func workdayCollect(t *testing.T, jobs internal.Jobs) ([]*internal.JobPosting, []error) {
	t.Helper()

	type outcome struct {
		postings []*internal.JobPosting
		errs     []error
	}

	done := make(chan outcome, 1)

	go func() {
		postings, errs := drain(jobs)
		done <- outcome{postings: postings, errs: errs}
	}()

	select {
	case got := <-done:
		return got.postings, got.errs
	case <-time.After(workdayStallGuard):
		t.Fatalf("workday adapter did not finish within %s; it is stalled, not slow", workdayStallGuard)

		return nil, nil
	}
}

// TestWorkdayClosesEveryPageBody is a regression test.
//
// Every page's body used to be closed by a `defer` inside the pagination loop,
// so none of them were closed until the whole source function returned. httpx
// releases a service's concurrency slot only when the body is closed, so the
// leak was not a memory problem, it was a deadlock.
func TestWorkdayClosesEveryPageBody(t *testing.T) {
	t.Parallel()

	// A tenant that clamps to 20 no matter what page size is requested, which is
	// the shape that made the leak so consistent in production.
	stub := &workdayStub{total: 250, served: 20}

	postings, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 250, postings)

	got := stub.state()

	// 13 pages: the first plus twelve at a stride of 20.
	must.Len(t, 13, got.requests)
	test.Eq(t, len(got.requests), got.opened, test.Sprintf("every request should have produced a body"))
	test.Eq(t, got.opened, got.closed, test.Sprintf("%d bodies opened but %d closed; a page body was leaked", got.opened, got.closed))

	// The real invariant, and the one the old adapter broke: a page's body is
	// closed before the next page is fetched, so no more bodies are ever open at
	// once than there are fetchers. Balanced totals alone would not catch it,
	// because a loop-scoped `defer` does close every body, just far too late.
	must.LessEq(t, workdayPageFetchers, got.peakOpen,
		must.Sprintf("%d page bodies were open at the same time; each page must be closed before the next is fetched", got.peakOpen))
}

// TestWorkdayFetchesPastThePerServiceLimit is the regression test for the
// truncation this whole change exists to fix.
//
// Run against the old adapter it returns exactly 80 postings (four pages of
// twenty, one per httpx concurrency slot) and then blocks on the semaphore until
// the client's two-minute timeout, so it fails both on the count and on the
// stall guard. It goes through httpx.NewClient on purpose: the real client is
// what makes the leak fatal, and a bare transport would not reproduce it.
func TestWorkdayFetchesPastThePerServiceLimit(t *testing.T) {
	t.Parallel()

	const total = 500

	stub := &workdayStub{total: total, served: 20}
	client := httpx.NewClient(httpx.WithTransport(stub))

	postings, errs := workdayCollect(t, Workday(t.Context(), client, workdayTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, total, postings)

	// Pages arrive out of order, so assert on the set rather than the sequence:
	// every posting the tenant advertised must be present exactly once.
	seen := make(map[string]int, total)
	for _, posting := range postings {
		seen[posting.URL]++

		test.Eq(t, "acme", posting.Company)
	}

	must.MapLen(t, total, seen, must.Sprintf("expected %d distinct postings", total))

	for i := range total {
		want := fmt.Sprintf("%s/job/%d", workdayTestTenant, i)

		test.Eq(t, 1, seen[want], test.Sprintf("posting %q should appear exactly once", want))
	}
}

// TestWorkdayStopsFetchingWhenConsumerStops covers the iterator contract: a
// consumer that breaks must not leave the adapter fetching, either during the
// call or after it has returned.
func TestWorkdayStopsFetchingWhenConsumerStops(t *testing.T) {
	t.Parallel()

	// 200 pages are available; the consumer wants 25 postings.
	stub := &workdayStub{total: 4000, served: 20}

	var consumed int

	for posting, err := range Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant) {
		must.NoError(t, err)
		must.NotNil(t, posting)

		consumed++
		if consumed == 25 {
			break
		}
	}

	got := stub.state()

	// The results channel is unbuffered and each fetcher holds its slot until
	// its page has been handed over, so at the moment the consumer breaks (part
	// way through the second page) at most one slot beyond the initial
	// workdayPageFetchers can have been refilled.
	must.LessEq(t, 2+workdayPageFetchers, len(got.requests),
		must.Sprintf("made %d requests after consuming 25 of 4000 postings", len(got.requests)))

	// The adapter drains its fetchers before returning, so nothing may still be
	// in flight, and no body still open, once the loop is over.
	settled := len(got.requests)

	time.Sleep(100 * time.Millisecond)

	got = stub.state()

	test.Eq(t, settled, len(got.requests), test.Sprintf("a fetcher outlived the iterator"))
	test.Eq(t, got.opened, got.closed, test.Sprintf("%d bodies opened but %d closed on the early-stop path", got.opened, got.closed))
}

// TestWorkdayTerminatesWhenTenantIgnoresOffset covers the hard safety bound.
//
// Page offsets are derived from the total the tenant reports, so a tenant that
// reports an absurd total and serves page one forever would otherwise keep
// fetching until the crawl deadline.
func TestWorkdayTerminatesWhenTenantIgnoresOffset(t *testing.T) {
	t.Parallel()

	stub := &workdayStub{total: 1_000_000, served: 20, ignoreOffset: true}

	postings, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.SliceEmpty(t, errs)

	got := stub.state()

	must.Len(t, workdayMaxPages, got.requests, must.Sprintf("a tenant that ignores offset must stop at the page bound"))
	must.Len(t, workdayMaxPages*20, postings)
	test.Eq(t, got.opened, got.closed, test.Sprintf("%d bodies opened but %d closed", got.opened, got.closed))
	must.LessEq(t, workdayPageFetchers, got.peakOpen,
		must.Sprintf("%d page bodies were open at the same time", got.peakOpen))
}

// TestWorkdayStopsOnAnEmptyPage covers a tenant whose reported total overshoots
// what it will actually serve: the offsets past the end must not be requested
// once a page comes back empty.
func TestWorkdayStopsOnAnEmptyPage(t *testing.T) {
	t.Parallel()

	// Advertises 4000 postings but only ever hands out 100 of them.
	stub := &workdayStub{total: 4000, served: 20, serveUpTo: 100}

	postings, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 100, postings)

	got := stub.state()

	// Five pages hold every real posting, but the tenant's total advertises 200.
	// The first empty page stops further scheduling, and because the results
	// channel is unbuffered the consumer must reach that empty page no later
	// than its fifth result; each result it takes frees one slot and so admits
	// at most one more prefetch. The exact figure therefore depends on how many
	// prefetches were already in flight, which is why this asserts a small
	// constant rather than an exact count. What matters is that it is nowhere
	// near the 200 pages the advertised total would otherwise ask for.
	must.LessEq(t, 5+2*workdayPageFetchers, len(got.requests),
		must.Sprintf("made %d requests for a tenant with 5 real pages", len(got.requests)))

	test.Eq(t, got.opened, got.closed, test.Sprintf("%d bodies opened but %d closed", got.opened, got.closed))
}

// TestWorkdayFallsBackToTheBaselinePageSize covers the risk taken by asking for
// a larger page than Workday's own careers UI does: a tenant that refuses the
// bigger window must degrade to the documented one instead of dropping out of
// the crawl entirely.
func TestWorkdayFallsBackToTheBaselinePageSize(t *testing.T) {
	t.Parallel()

	stub := &workdayStub{total: 30, rejectLimitAbove: workdayBaselinePageSize}

	postings, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 30, postings)

	got := stub.state()

	must.GreaterEq(t, 2, len(got.requests))
	test.Eq(t, workdayPageSize, got.requests[0].limit, test.Sprintf("the first attempt should ask for the larger window"))
	test.Eq(t, workdayBaselinePageSize, got.requests[1].limit, test.Sprintf("the retry should fall back to the baseline window"))
	test.Eq(t, 0, got.requests[1].offset, test.Sprintf("the retry is still page one"))
	test.Eq(t, got.opened, got.closed, test.Sprintf("%d bodies opened but %d closed on the fallback path", got.opened, got.closed))
}

// TestWorkdayBoundsPageConcurrency guards the politeness promise: fetching
// pages in parallel must not put more load on a tenant than httpx would allow
// anyway.
func TestWorkdayBoundsPageConcurrency(t *testing.T) {
	t.Parallel()

	stub := &workdayStub{total: 400, served: 20, delay: 5 * time.Millisecond}

	postings, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 400, postings)

	got := stub.state()

	must.LessEq(t, workdayPageFetchers, got.peak,
		must.Sprintf("peak concurrency %d exceeds the bound of %d", got.peak, workdayPageFetchers))
	must.GreaterEq(t, 2, got.peak, must.Sprintf("pages were fetched one at a time; the fan-out is not working"))
}

// TestWorkdayReadsPostedOnAndBulletFields is a regression test.
//
// postedOn and bulletFields have been decoded into workdayInfo since this
// adapter was written and were never read, on every page of every one of the
// ~216 tenants.
func TestWorkdayReadsPostedOnAndBulletFields(t *testing.T) {
	t.Parallel()

	stub := &workdayStub{total: 3, served: 3}

	postings, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	today := time.Now().UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	for _, posting := range postings {
		test.Eq(t, today, posting.PostedAt)
		test.Eq(t, internal.EmploymentTypeFullTime, posting.EmploymentType)
		test.Eq(t, internal.PostingSource{Platform: workdayPlatform, Key: workdayTestTenant}, posting.Source)
	}

	test.Eq(t, "JR0", postings[0].RequisitionID)
}

func TestWorkdayPostedAt(t *testing.T) {
	t.Parallel()

	// A fixed clock, because the whole point of passing "now" in is that one
	// crawl dates every posting from a single instant and that this is testable
	// without a fake clock package.
	now := time.Date(2026, time.July, 26, 15, 30, 0, 0, time.UTC)
	day := time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{in: "Posted Today", want: day, ok: true},
		{in: "Posted Yesterday", want: day.AddDate(0, 0, -1), ok: true},
		{in: "Posted 5 Days Ago", want: day.AddDate(0, 0, -5), ok: true},
		{in: "Posted 1 Day Ago", want: day.AddDate(0, 0, -1), ok: true},
		{in: "posted 5 days ago", want: day.AddDate(0, 0, -5), ok: true},
		{in: "Today", want: day, ok: true},

		// "30+" is a lower bound covering everything from a month to three
		// years. Recording it as exactly thirty days would hand
		// Filter.PostedSince a date this project made up, and no consumer could
		// tell it from one the employer published.
		{in: "Posted 30+ Days Ago"},

		{in: ""},
		{in: "Posted Recently"},
		{in: "Veröffentlicht vor 5 Tagen"},
		{in: "Posted 99999 Days Ago"},
	}

	for _, tt := range tests {
		t.Run(cmp.Or(tt.in, "empty"), func(t *testing.T) {
			t.Parallel()

			got, ok := workdayPostedAt(tt.in, now)

			test.Eq(t, tt.ok, ok)
			test.Eq(t, tt.want, got)
		})
	}
}

func TestWorkdayRequisitionID(t *testing.T) {
	t.Parallel()

	// Workday does not label its bullets, so the requisition number has to be
	// recognised by shape. Every rejection below is a real thing tenants put in
	// this list, and filling the field with one of them would be worse than
	// leaving it empty: a wrong requisition number is what someone quotes to a
	// recruiter.
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "none", in: nil},
		{name: "typical", in: []string{"JR0012345"}, want: "JR0012345"},
		{name: "hyphenated", in: []string{"R-00012345", "Full time"}, want: "R-00012345"},
		{name: "after a time type", in: []string{"Full time", "REQ-4821"}, want: "REQ-4821"},
		{name: "job family", in: []string{"Engineering"}},
		{name: "time type", in: []string{"Full time"}},
		{name: "site", in: []string{"San Francisco, CA"}},
		{name: "bare date", in: []string{"2024-01-15"}},
		{name: "all digits", in: []string{"1234567"}},
		{name: "too short", in: []string{"R1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tt.want, workdayRequisitionID(tt.in))
		})
	}
}

func TestWorkdayEmploymentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want internal.EmploymentType
	}{
		{name: "none", in: nil},
		{name: "full time", in: []string{"JR001", "Full time"}, want: internal.EmploymentTypeFullTime},
		{name: "part time", in: []string{"Part time"}, want: internal.EmploymentTypePartTime},
		{name: "intern", in: []string{"Intern"}, want: internal.EmploymentTypeInternship},

		// The gate exists for this. NormalizeEmploymentType matches a
		// distinctive word inside a value on purpose, which is right when the
		// board says the field is an employment type and wrong here, where the
		// bullet could be anything the tenant chose to display.
		{name: "job family containing a keyword", in: []string{"Contracts Management"}},

		// Tenure, not hours. A permanent part-time role is an ordinary thing.
		{name: "tenure", in: []string{"Regular"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tt.want, workdayEmploymentType(tt.in))
		})
	}
}

// TestWorkdayReportsAPageFailure keeps failures attributable: a page that fails
// mid-crawl must surface an error naming the tenant, not disappear.
func TestWorkdayReportsAPageFailure(t *testing.T) {
	t.Parallel()

	stub := &workdayStub{total: 400, served: 20, failFromOffset: 100}

	_, errs := workdayCollect(t, Workday(t.Context(), &http.Client{Transport: stub}, workdayTestTenant))

	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), workdayTestTenant)

	got := stub.state()

	test.Eq(t, got.opened, got.closed, test.Sprintf("%d bodies opened but %d closed on the failure path", got.opened, got.closed))
}
