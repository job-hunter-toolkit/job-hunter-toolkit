package services

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// phenomFixturePage wraps job entries in the HTML shell a Phenom search-results
// page actually serves: the search payload sits inside a "phApp.ddo" script
// assignment, not as a bare JSON document.
func phenomFixturePage(jobsJSON string) string {
	return `<html><body><script>var phApp = phApp || {};
phApp.ddo = {"siteConfig":{"status":"success"},"eagerLoadRefineSearch":{"status":200,"hits":1,"totalHits":1,"data":{"jobs":[` +
		jobsJSON + `]}},"eid":{"eid":"abc123"}};</script></body></html>`
}

func TestPhenomParsesPostings(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"acme.example.com": phenomFixturePage(strings.Join([]string{
			`{"title":"  Security Engineer  ","cityState":"  Remote - US  ","applyUrl":"https://acme.wd1.myworkdayjobs.com/apply/1"}`,
			// No applyUrl: some tenants handle applications on the Phenom
			// site itself, so the URL must be constructed from the job ID.
			`{"title":"Detection Engineer","jobId":"12345","applyUrl":""}`,
			// No title: not actionable, so it is dropped.
			`{"title":"","jobId":"99999","applyUrl":"https://acme.example.com/apply/skip-me"}`,
			// Neither an apply URL nor a job ID to build one from: dropped.
			`{"title":"No URL","jobId":"","applyUrl":""}`,
			// No cityState: falls back to the "location" field.
			`{"title":"Fallback Location","location":"Chicago, IL","applyUrl":"https://acme.example.com/apply/2"}`,
		}, ",")),
	})

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 3 {
		t.Fatalf("got %d postings, want 3 (incomplete ones are skipped)", len(postings))
	}

	if got, want := postings[0].Title, "Security Engineer"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}

	if got, want := postings[0].Location, "Remote - US"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	if got, want := postings[0].Company, "example"; got != want {
		t.Errorf("Company = %q, want %q", got, want)
	}

	if got, want := postings[1].URL, "https://acme.example.com/us/en/job/12345"; got != want {
		t.Errorf("URL = %q, want %q (constructed from the job ID)", got, want)
	}

	if got, want := postings[2].Location, "Chicago, IL"; got != want {
		t.Errorf("Location = %q, want %q (from the \"location\" fallback field)", got, want)
	}

	// A short page (well under phenomPageSize) ends pagination after one request.
	if len(transport.requests) != 1 {
		t.Errorf("made %d requests, want 1 (a short page ends pagination)", len(transport.requests))
	}
}

func TestPhenomPaginatesUntilAShortPage(t *testing.T) {
	t.Parallel()

	fullPage := make([]string, phenomPageSize)
	for i := range fullPage {
		fullPage[i] = fmt.Sprintf(`{"title":"Job %d","jobId":"%d","applyUrl":"https://acme.example.com/apply/%d"}`, i, i, i)
	}

	client, transport := fixtureClient(map[string]string{
		"from=0&size=" + strconv.Itoa(phenomPageSize):                                    phenomFixturePage(strings.Join(fullPage, ",")),
		"from=" + strconv.Itoa(phenomPageSize) + "&size=" + strconv.Itoa(phenomPageSize): phenomFixturePage(`{"title":"Last Job","jobId":"last","applyUrl":"https://acme.example.com/apply/last"}`),
	})

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if got, want := len(postings), phenomPageSize+1; got != want {
		t.Fatalf("got %d postings, want %d", got, want)
	}

	// A full first page must not be mistaken for the end of results: a second,
	// shorter page has to be requested before pagination stops.
	if len(transport.requests) != 2 {
		t.Errorf("made %d requests, want 2 (a full page keeps paging)", len(transport.requests))
	}
}

// TestPhenomStopsWhenTheSiteIgnoresFrom is a regression test.
//
// This adapter re-requests the *same* server-rendered search-results page with a
// different "from", which makes it the likeliest platform here to meet a tenant
// whose SSR ignores the offset and serves identical jobs forever. Termination
// used to be decided solely by page size, so such a tenant was crawled until the
// crawl deadline: measured at 5,001 requests and 500,001 duplicate postings
// against a stub like this one in 0.9 seconds.
func TestPhenomStopsWhenTheSiteIgnoresFrom(t *testing.T) {
	t.Parallel()

	fullPage := make([]string, phenomPageSize)
	for i := range fullPage {
		fullPage[i] = fmt.Sprintf(`{"title":"Job %d","jobId":"%d","applyUrl":"https://acme.example.com/apply/%d"}`, i, i, i)
	}

	client, transport := repeatingPageClient(phenomFixturePage(strings.Join(fullPage, ",")))

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, phenomPageSize, postings)
}

// TestPhenomStopsWhenTheSiteIgnoresFromWithoutApplyURLs covers the same tenant
// behaviour on a board that handles applications on the Phenom site itself.
// Those postings carry no applyUrl at all, so a page fingerprint taken from
// apply URLs alone would be an empty list on every page and could not tell two
// pages apart.
func TestPhenomStopsWhenTheSiteIgnoresFromWithoutApplyURLs(t *testing.T) {
	t.Parallel()

	fullPage := make([]string, phenomPageSize)
	for i := range fullPage {
		fullPage[i] = fmt.Sprintf(`{"title":"Job %d","jobId":"%d","applyUrl":""}`, i, i)
	}

	client, transport := repeatingPageClient(phenomFixturePage(strings.Join(fullPage, ",")))

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	must.SliceEmpty(t, errs)
	test.Eq(t, 2, transport.requests)
	test.Len(t, phenomPageSize, postings)
}

// TestPhenomStopsWhenTheConsumerDoes guards the iterator contract the health
// command depends on: it caps each source at 100 postings by returning false
// from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
func TestPhenomStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	fullPage := make([]string, phenomPageSize)
	for i := range fullPage {
		fullPage[i] = fmt.Sprintf(`{"title":"Job %d","jobId":"%d","applyUrl":"https://acme.example.com/apply/%d"}`, i, i, i)
	}

	client, transport := repeatingPageClient(phenomFixturePage(strings.Join(fullPage, ",")))

	var seen int

	for range Phenom(t.Context(), client, "acme.example.com") {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, transport.requests)
}

func TestPhenomReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"acme.example.com": phenomFixturePage("")},
		status: http.StatusTooManyRequests,
	}
	client := &http.Client{Transport: transport}

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}

	if !strings.Contains(errs[0].Error(), "acme.example.com") {
		t.Errorf("error = %v, want it to name the company", errs[0])
	}
}

func TestPhenomReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.example.com": `<html><script>phApp.ddo = {"eagerLoadRefineSearch":{ this is not json` + "</script></html>",
	})

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}

	if !strings.Contains(errs[0].Error(), "acme.example.com") {
		t.Errorf("error = %v, want it to name the company", errs[0])
	}
}

// TestPhenomReportsMissingSearchResults is a regression guard for the
// approach itself: this adapter depends on Phenom continuing to embed the
// search payload in the page rather than serving it from a plain JSON
// endpoint. If a tenant's page stops containing it, that must surface as an
// attributable error instead of a silent, empty result.
func TestPhenomReportsMissingSearchResults(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.example.com": `<html><body>no search payload here</body></html>`,
	})

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0", len(postings))
	}

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}

	if !strings.Contains(errs[0].Error(), "acme.example.com") {
		t.Errorf("error = %v, want it to name the company", errs[0])
	}
}

func TestPhenomCompanyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "careers.southwestair.com", want: "southwestair"},
		{in: "talent.lowes.com", want: "lowes"},
		{in: "jobs.bechtel.com", want: "bechtel"},
		{in: "careers.kbr.com", want: "kbr"},
		{in: "onlylabel", want: "onlylabel"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := phenomCompanyName(tt.in); got != tt.want {
				t.Errorf("phenomCompanyName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
