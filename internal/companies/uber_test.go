package companies

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	jobpostings "github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestUber(t *testing.T) {
	t.Parallel()
	tests.RequireNetwork(t)

	var found int

	for jobPosting, err := range Uber(t.Context(), httpx.NewClient()) {
		must.NoError(t, err)
		tests.CheckJobPosting(t, jobPosting)

		found++
	}

	t.Logf("found %d job postings for Uber", found)
}

// uberSearchStub answers the search endpoint with a full page of results, so a
// short page can never be what ends a pagination loop under test.
//
// With repeat set it serves the same results whatever page is asked for, which
// is what a search backend that ignores its "page" parameter does. It fails
// after maxRequests, so an adapter that has lost its pagination bound fails its
// test in milliseconds and says why, rather than looping until the test binary's
// timeout.
type uberSearchStub struct {
	// total is reported as totalResults.low.
	total       int
	repeat      bool
	maxRequests int
	requests    int
}

func (u *uberSearchStub) RoundTrip(req *http.Request) (*http.Response, error) {
	u.requests++

	if u.requests > u.maxRequests {
		return nil, fmt.Errorf("made %d requests against a search that never advances: pagination is unbounded", u.requests)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	var request struct {
		Page int `json:"page"`
	}

	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("stub could not decode the search request: %w", err)
	}

	page := request.Page
	if u.repeat {
		page = 0
	}

	results := make([]string, uberPageSize)
	for i := range results {
		id := page*uberPageSize + i
		results[i] = fmt.Sprintf(`{"id":%d,"title":"Job %d","location":{"country":"US","region":"CA","city":"San Francisco"}}`, id, id)
	}

	response := fmt.Sprintf(
		`{"data":{"results":[%s],"totalResults":{"low":%d,"high":0,"unsigned":false}}}`,
		strings.Join(results, ","), u.total,
	)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(response)),
		Request:    req,
	}, nil
}

// TestUberReadsTheFieldsItAlreadyDownloads is a regression test.
//
// Uber's search returns the richest per-posting payload in this project and the
// struct commented every field of it out, so the decoder threw the lot away on
// every request. Nothing here costs a request or a byte.
func TestUberReadsTheFieldsItAlreadyDownloads(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"data":{"results":[
				{
					"id": 141234,
					"title": "Senior Security Engineer",
					"department": "Engineering",
					"team": "Application Security",
					"type": "Regular",
					"timeType": "Full Time",
					"level": "5a",
					"creationDate": "2026-06-01T12:00:00Z",
					"updatedDate": "2026-07-01T09:30:00Z",
					"location": {"country": "US", "region": "CA", "city": "San Francisco"},
					"managerFirstName": "Ada",
					"managerEmail": "ada@example.com",
					"recruiterEmail": "grace@example.com"
				},
				{
					"id": 141235,
					"title": "Summer Intern",
					"type": "Intern",
					"creationDate": "not a date",
					"location": {"country": "US", "region": "CA", "city": "San Francisco"}
				}
			],"totalResults":{"low":2,"high":0,"unsigned":false}}}`)),
			Request: req,
		}, nil
	})}

	var postings []*jobpostings.JobPosting

	for posting, err := range Uber(t.Context(), client) {
		must.NoError(t, err)

		postings = append(postings, posting)
	}

	must.Len(t, 2, postings)

	test.Eq(t, "Engineering", postings[0].Department)
	test.Eq(t, "Application Security", postings[0].Team)
	test.Eq(t, "5a", postings[0].Seniority)
	test.Eq(t, "141234", postings[0].ExternalID)
	test.Eq(t, jobpostings.PostingSource{Platform: DirectPlatform, Key: "uber"}, postings[0].Source)
	test.Eq(t, time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC), postings[0].PostedAt)
	test.Eq(t, time.Date(2026, time.July, 1, 9, 30, 0, 0, time.UTC), postings[0].UpdatedAt)

	// timeType is the hours; "Regular" is tenure and says nothing about them, so
	// the hours field answers first and the tenure field only fills gaps.
	test.Eq(t, jobpostings.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, jobpostings.EmploymentTypeInternship, postings[1].EmploymentType)

	// A date this adapter cannot read stays absent rather than becoming the
	// crawl time, which would date every Uber posting to today.
	test.True(t, postings[1].PostedAt.IsZero())

	// The manager and recruiter blocks stay unmodelled: they are the personal
	// data of people who did not publish a job, and capturing them into output
	// that reaches stdout, jobs_record.txt and shell history is a decision to be
	// taken deliberately, not because the bytes happened to be free.
	marshalled, err := json.Marshal(postings[0])
	must.NoError(t, err)
	test.StrNotContains(t, string(marshalled), "example.com")
	test.StrNotContains(t, string(marshalled), "Ada")
}

// TestUberReportsAFailedPage is a regression test.
//
// This adapter's signature carried no error channel, so every page failure was
// swallowed and an unreachable search endpoint was indistinguishable from an
// employer with nothing open. A silently-empty source is the worst failure this
// project has, and it is the reason the crawl reports errors per source at all.
func TestUberReportsAFailedPage(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     http.StatusText(http.StatusForbidden),
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"error":"nope"}`)),
			Request:    req,
		}, nil
	})}

	var (
		postings []*jobpostings.JobPosting
		errs     []error
	)

	for posting, err := range Uber(t.Context(), client) {
		if err != nil {
			errs = append(errs, err)

			continue
		}

		postings = append(postings, posting)
	}

	must.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "Uber")
}

// roundTripFunc adapts a function to [net/http.RoundTripper].
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestUberStopsWhenTheSearchIgnoresPage is a regression test.
//
// Termination used to be decided solely by a page coming back empty, so a search
// backend that answers every "page" with the same results was crawled until the
// crawl deadline. The sibling adapters in internal/services had the same shape
// and drew 5,001 requests and 500,001 duplicate postings out of a stub like this
// one in under a second.
//
// The reported total here is far larger than anything the stub will serve, so
// the repeated-page check is the only thing that can end this crawl.
func TestUberStopsWhenTheSearchIgnoresPage(t *testing.T) {
	t.Parallel()

	stub := &uberSearchStub{total: 500_000, repeat: true, maxRequests: 50}

	var found int

	for _, err := range Uber(t.Context(), &http.Client{Transport: stub}) {
		must.NoError(t, err)

		found++
	}

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, stub.requests)
	test.Eq(t, uberPageSize, found)
}

// TestUberStopsAtItsReportedTotal covers the stop the API actually offers: the
// search reports how many postings it matched, in a field that was modelled and
// then commented out.
func TestUberStopsAtItsReportedTotal(t *testing.T) {
	t.Parallel()

	// Exactly two full pages, each one different, so neither the short-page
	// check nor the repeated-page check can fire.
	stub := &uberSearchStub{total: 2 * uberPageSize, maxRequests: 50}

	var found int

	for _, err := range Uber(t.Context(), &http.Client{Transport: stub}) {
		must.NoError(t, err)

		found++
	}

	test.Eq(t, 2, stub.requests)
	test.Eq(t, 2*uberPageSize, found)
}

// TestUberStopsWhenTheConsumerDoes guards the iterator contract: a consumer that
// stops wanting postings, which is how the health command caps a source at 100,
// must stop the fetching too.
func TestUberStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	stub := &uberSearchStub{total: 500_000, repeat: true, maxRequests: 50}

	var seen int

	for _, err := range Uber(t.Context(), &http.Client{Transport: stub}) {
		must.NoError(t, err)

		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, stub.requests)
}
