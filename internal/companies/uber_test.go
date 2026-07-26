package companies

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestUber(t *testing.T) {
	t.Parallel()
	tests.RequireNetwork(t)

	jobPostings, err := Uber(t.Context(), httpx.NewClient())
	must.NoError(t, err)

	var found int

	for jobPosting := range jobPostings {
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

	postings, err := Uber(t.Context(), &http.Client{Transport: stub})
	must.NoError(t, err)

	var found int

	for range postings {
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

	postings, err := Uber(t.Context(), &http.Client{Transport: stub})
	must.NoError(t, err)

	var found int

	for range postings {
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

	postings, err := Uber(t.Context(), &http.Client{Transport: stub})
	must.NoError(t, err)

	var seen int

	for range postings {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, stub.requests)
}
