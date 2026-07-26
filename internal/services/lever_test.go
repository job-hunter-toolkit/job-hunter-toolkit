package services

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestLever(t *testing.T) {
	testSingle(t, "plaid", Lever)
}

func TestLever_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	testMultipleParallel(t, slices.Values(LeverCompanies), Lever)
}

// repeatingPageTransport answers every request with the same body, which is what
// a board that ignores its page or offset parameter does.
//
// After maxRequests it fails instead of answering. An adapter that has lost its
// pagination bound therefore fails its test in milliseconds with an error that
// says what went wrong, rather than looping until the test binary's timeout: the
// unbounded versions of these adapters drew 5,001 requests and 500,001 duplicate
// postings out of a stub like this one in under a second each.
type repeatingPageTransport struct {
	body        string
	maxRequests int
	requests    int
}

func (r *repeatingPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests++

	if r.requests > r.maxRequests {
		return nil, fmt.Errorf("made %d requests to %s against a board that never advances: pagination is unbounded", r.requests, req.URL)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

// repeatingPageClient returns a client that serves body for every request.
func repeatingPageClient(body string) (*http.Client, *repeatingPageTransport) {
	transport := &repeatingPageTransport{body: body, maxRequests: 50}

	return &http.Client{Transport: transport}, transport
}

// leverFullPage builds a page of exactly limit postings, so the short-page check
// cannot be what ends a pagination loop under test.
func leverFullPage(prefix string) string {
	page := make([]string, 100)

	for i := range page {
		page[i] = fmt.Sprintf(`{"text":"Job %s%d","hostedUrl":"https://jobs.lever.co/acme/%s%d","categories":{"location":"Remote"}}`, prefix, i, prefix, i)
	}

	return "[" + strings.Join(page, ",") + "]"
}

// TestLeverStopsWhenTheBoardIgnoresSkip is a regression test.
//
// Lever publishes no total, so this loop used to end only when a page came back
// short or empty. A board that answers every "skip" with the same full page
// never sends one, so the adapter paginated until the crawl deadline, pinning
// one of the crawl's worker slots and hammering a single host for hours, while
// internal.Dedupe hid the duplicate postings from the crawl total.
func TestLeverStopsWhenTheBoardIgnoresSkip(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(leverFullPage(""))

	postings, errs := drain(Lever(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, 100, postings)
}

// offsetEchoTransport serves a full Lever page whose posting URLs embed the
// offset they were served at, so no two pages are ever alike and only a hard
// ceiling can end the crawl.
type offsetEchoTransport struct {
	requests int
}

func (o *offsetEchoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	o.requests++

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(leverFullPage(req.URL.Query().Get("skip") + "-"))),
		Request:    req,
	}, nil
}

// TestLeverStopsAtItsPageCeiling covers the backstop for the case a repeated
// page cannot catch: a board that keeps serving different full pages forever.
// Hitting the ceiling is reported rather than passed off as the end of a board.
func TestLeverStopsAtItsPageCeiling(t *testing.T) {
	t.Parallel()

	transport := &offsetEchoTransport{}

	postings, errs := drain(Lever(t.Context(), &http.Client{Transport: transport}, "acme"))

	test.Eq(t, leverMaxPages, transport.requests)
	test.Len(t, leverMaxPages*100, postings)

	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "refusing to keep paginating")
}

// TestLeverStopsWhenTheConsumerDoes guards the iterator contract the health
// command depends on: it caps each source at 100 postings by returning false
// from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
func TestLeverStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(leverFullPage(""))

	var seen int

	for range Lever(t.Context(), client, "acme") {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, transport.requests)
}
