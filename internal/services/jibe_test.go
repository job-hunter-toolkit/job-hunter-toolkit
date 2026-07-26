package services

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

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
