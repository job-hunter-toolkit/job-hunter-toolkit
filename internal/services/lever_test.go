package services

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
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

// leverEnrichedPage is one page in the shape Lever already serves for the
// `?mode=json&limit=100&skip=N` URL this adapter already fetches.
//
// The first posting contradicts itself on purpose: its location says "Remote -
// US" while the board's structured workplaceType says on-site, and its closing
// block states a pay range different from its salaryRange. Both conflicts have a
// documented winner, and this fixture is what proves which one wins.
const leverEnrichedPage = `[
	{
		"id": "0f3a3f11-0000-4000-8000-000000000001",
		"text": "Senior Detection Engineer",
		"hostedUrl": "https://jobs.lever.co/acme/1",
		"createdAt": 1767225600000,
		"workplaceType": "on-site",
		"categories": {
			"commitment": "Full-time",
			"department": "Engineering",
			"team": "Detection",
			"level": "Senior",
			"location": "Remote - US"
		},
		"descriptionPlain": "We are hiring a detection engineer.",
		"additionalPlain": "The base salary range for this role is $180,000 - $220,000 per year.",
		"salaryRange": {"min": 150000, "max": 200000, "currency": "USD", "interval": "per-year-salary"}
	},
	{
		"id": "0f3a3f11-0000-4000-8000-000000000002",
		"text": "Contract Recruiter",
		"hostedUrl": "https://jobs.lever.co/acme/2",
		"createdAt": 0,
		"workplaceType": "unspecified",
		"categories": {
			"commitment": "Contract",
			"department": "People",
			"team": "",
			"level": "",
			"location": "Remote"
		},
		"descriptionPlain": "A short engagement supporting the security team.",
		"additionalPlain": "The hourly rate for this contract is $75 - $95 per hour."
	}
]`

// TestLeverDecodesTheFieldsAlreadyOnTheWire covers the enrichment fields.
//
// All of these used to sit commented out in leverJobs, which is a record that
// somebody saw them in a real response. They ride in the page the adapter
// already downloads, so decoding them adds no request to any of the 161 Lever
// sources.
func TestLeverDecodesTheFieldsAlreadyOnTheWire(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{"api.lever.co": leverEnrichedPage})

	postings, errs := drain(Lever(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// A short page ends pagination, so enrichment cost no extra request.
	must.Len(t, 1, transport.requests)

	engineer, recruiter := postings[0], postings[1]

	test.Eq(t, "Engineering", engineer.Department)
	test.Eq(t, "Detection", engineer.Team)
	test.Eq(t, "Senior", engineer.Seniority)
	test.Eq(t, internal.EmploymentTypeFullTime, engineer.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeOnsite, engineer.WorkplaceType)
	test.Eq(t, "0f3a3f11-0000-4000-8000-000000000001", engineer.ExternalID)
	test.Eq(t, internal.PostingSource{Platform: "lever", Key: "acme"}, engineer.Source)

	// createdAt is epoch milliseconds. Read as seconds this exact value dates the
	// posting to the year 57971 and satisfies every --posted-since ever asked.
	test.Eq(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), engineer.PostedAt)
	test.Eq(t, "UTC", engineer.PostedAt.Location().String())

	// The board says on-site, so the word "Remote" in the location text no
	// longer decides the question. The board's own answer beats the heuristic.
	test.False(t, engineer.IsRemote())

	// "unspecified" is deliberately unrecognised: it is the employer declining
	// to say, not a statement that an office is required. The field stays empty
	// and IsRemote falls back to the location text, as it did before.
	test.Eq(t, internal.WorkplaceTypeUnknown, recruiter.WorkplaceType)
	test.Nil(t, recruiter.Remote)
	test.True(t, recruiter.IsRemote())

	test.Eq(t, internal.EmploymentTypeContract, recruiter.EmploymentType)
	test.Eq(t, "People", recruiter.Department)
	test.Eq(t, "", recruiter.Team)

	// createdAt absent means no date, not the epoch.
	test.True(t, recruiter.PostedAt.IsZero())
}

// TestLeverPrefersEmployerPayOverProse is the provenance guard, the twin of the
// Ashby one: salaryRange is sparsely filled, and the body that would have
// carried the same number in a sentence is already downloaded.
func TestLeverPrefersEmployerPayOverProse(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"api.lever.co": leverEnrichedPage})

	postings, errs := drain(Lever(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// salaryRange says 150k-200k and the closing block says 180k-220k. The
	// employer's structured field wins, and keeps its own provenance.
	employer := postings[0].Compensation
	must.NotNil(t, employer)
	test.Eq(t, internal.ProvenanceEmployer, employer.Provenance)
	test.Eq(t, 150000.0, employer.Min)
	test.Eq(t, 200000.0, employer.Max)

	// No salaryRange here, so the closing block fills the gap. Lever splits a
	// posting in two, and the pay-transparency paragraph is conventionally in
	// additionalPlain rather than descriptionPlain, so both are scanned.
	prose := postings[1].Compensation
	must.NotNil(t, prose)
	test.Eq(t, internal.ProvenanceDescription, prose.Provenance)
	test.Eq(t, 75.0, prose.Min)
	test.Eq(t, 95.0, prose.Max)
	test.Eq(t, internal.PeriodHour, prose.Period)
}

func TestLeverCreatedAt(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		millis int64
		want   time.Time
	}{
		"epoch milliseconds": {
			millis: 1767225600000,
			want:   time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		// The whole point of the helper: the same number read as seconds is a
		// date 56,000 years out, and a wrong date cannot be told apart from a
		// right one by anything downstream.
		"sub-second precision is kept": {
			millis: 1767225600123,
			want:   time.Date(2026, time.January, 1, 0, 0, 0, 123_000_000, time.UTC),
		},
		"absent": {millis: 0, want: time.Time{}},
		// Not a pre-1970 posting; a board sending a negative here is broken.
		"negative": {millis: -1, want: time.Time{}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := leverCreatedAt(tt.millis)

			test.Eq(t, tt.want, got)

			if !tt.want.IsZero() {
				test.Eq(t, "UTC", got.Location().String())
			}
		})
	}
}

func TestLeverDescription(t *testing.T) {
	t.Parallel()

	// The blank line matters: the extractor reads a fixed window of characters
	// before a money figure to decide whether it is pay, so the last sentence of
	// one block must not run into the first sentence of the next.
	test.Eq(t, "opening\n\nclosing", leverDescription("  opening  ", "closing"))
	test.Eq(t, "closing", leverDescription("", "closing"))
	test.Eq(t, "opening", leverDescription("opening", "   "))
	test.Eq(t, "", leverDescription("", ""))
}
