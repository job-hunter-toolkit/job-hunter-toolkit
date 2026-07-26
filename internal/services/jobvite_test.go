package services

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestJobvite(t *testing.T) {
	testSingle(t, "splunk-careers", Jobvite)
}

func TestJobvite_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(JobviteCompanies), Jobvite)
}

func TestJobviteStartsAtPageZero(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"p=0": `<table><tr>` +
			`<td><a href="/acme/job/abc">Platform Engineer</a></td>` +
			`<td></td><td></td><td>Detroit, Michigan</td>` +
			`</tr></table>`,
		"p=1": `<p ng-non-bindable>No more results</p>`,
	})

	postings, errs := drain(Jobvite(t.Context(), client, "acme"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}

	if len(transport.requests) != 2 {
		t.Fatalf("made %d requests, want page zero and the empty page one", len(transport.requests))
	}

	if !strings.Contains(transport.requests[0], "p=0") {
		t.Errorf("first request = %q, want page zero", transport.requests[0])
	}
}

// TestJobviteReadsTheCellsItSteppedOver is a regression test.
//
// The walk from a job link to its location advanced exactly three siblings, so
// the two cells in between were parsed into the document, held in memory, and
// never looked at. They are read by content rather than by position because the
// row has no headers and tenants order their columns differently.
func TestJobviteReadsTheCellsItSteppedOver(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"p=0": `<table>` +
			`<tr>` +
			`<td><a href="/acme/job/oQ1Yjfwv">Platform Engineer</a></td>` +
			`<td>Full-time</td><td>Jan 6, 2026</td><td>Detroit, Michigan</td>` +
			`</tr>` +
			`<tr>` +
			`<td><a href="/acme/job/aB2Zkgxw?src=rss">Contracts Manager</a></td>` +
			`<td>Contracts Management</td><td>REQ-4821</td><td>Austin, Texas</td>` +
			`</tr>` +
			`</table>`,
		"p=1": `<p ng-non-bindable>No more results</p>`,
	})

	postings, errs := drain(Jobvite(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, time.Date(2026, time.January, 6, 0, 0, 0, 0, time.UTC), postings[0].PostedAt)
	test.Eq(t, "oQ1Yjfwv", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: jobvitePlatform, Key: "acme"}, postings[0].Source)

	// The location cell is unchanged: still the fourth cell of the row.
	test.Eq(t, "Detroit, Michigan", postings[0].Location)

	// A job family whose name contains "Contract" must not be read as contract
	// work. These cells are unlabelled, so the normalizer's substring matching —
	// right when a board says the field is an employment type — has to be gated
	// behind an exact match here.
	test.Eq(t, internal.EmploymentTypeUnknown, postings[1].EmploymentType)

	// A requisition number is not a date, and nothing else in the row is read at
	// all: "whatever is left over" is as likely to be a brand as a department.
	test.True(t, postings[1].PostedAt.IsZero())
	test.Eq(t, "", postings[1].Department)

	// Tracking parameters are not part of the identifier.
	test.Eq(t, "aB2Zkgxw", postings[1].ExternalID)
}

// jobviteSearchPage renders the rows of a Jobvite search result the way the
// adapter expects to find them: the location sits in the fourth cell of the row
// holding the job link.
func jobviteSearchPage(rows int) string {
	page := &strings.Builder{}

	page.WriteString("<table>")

	for i := range rows {
		fmt.Fprintf(page, `<tr><td><a href="/acme/job/%d">Job %d</a></td><td></td><td></td><td>Detroit, Michigan</td></tr>`, i, i)
	}

	page.WriteString("</table>")

	return page.String()
}

// TestJobviteStopsWhenTheSearchIgnoresThePageParameter is a regression test.
//
// Jobvite publishes no total, so this loop used to end only when a page happened
// to contain no job links or carried the end-of-results marker. A tenant that
// answers every "p" with the same rows sends neither, so the adapter paginated
// until the crawl deadline, the same shape that produced 5,001 requests and
// 500,001 duplicate postings from the sibling adapters against a stub.
func TestJobviteStopsWhenTheSearchIgnoresThePageParameter(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(jobviteSearchPage(25))

	postings, errs := drain(Jobvite(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, 25, postings)
}

// TestJobviteStopsWhenTheConsumerDoes guards the iterator contract the health
// command depends on: it caps each source at 100 postings by returning false
// from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
//
// This adapter is the one that already panicked that way once, which is why its
// tree walk used to thread a "stopped" flag through every level of the
// recursion; postings are now collected before any is yielded, so the walk
// cannot call yield at all.
func TestJobviteStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(jobviteSearchPage(25))

	var seen int

	for range Jobvite(t.Context(), client, "acme") {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, transport.requests)
}
