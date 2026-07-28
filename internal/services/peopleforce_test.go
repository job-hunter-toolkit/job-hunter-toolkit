package services

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPeopleForce(t *testing.T) {
	testSingle(t, "kagi", PeopleForce)
}

// peopleForceTransport serves careers pages keyed by the exact request URL.
//
// Exact rather than by substring, as [fixtureTransport] does, because page one
// of a PeopleForce board is "…/careers" with no query at all: every substring of
// it is also a substring of "…/careers?page=2", so substring routing cannot tell
// the two apart.
//
// Unmapped URLs are answered with 404, which is one of the two ways a board says
// a page number is past the end.
type peopleForceTransport struct {
	pages    map[string]string
	requests []string
}

func (p *peopleForceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p.requests = append(p.requests, req.URL.String())

	body, ok := p.pages[req.URL.String()]

	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// peopleForceCareersPage renders a careers page the way the adapter expects to
// find one: job links inside a container with id="results", each with a sibling
// div holding "<department>, <employment type>, <location>".
//
// displaying is inserted verbatim, so a test can supply the "Displaying X - Y of
// Z in total" marker or leave it out entirely.
func peopleForceCareersPage(displaying string, slugs ...string) string {
	page := &strings.Builder{}

	page.WriteString("<html><body>")
	page.WriteString(displaying)
	page.WriteString(`<div id="results">`)

	for _, slug := range slugs {
		fmt.Fprintf(page,
			`<div class="row"><div class="title"><a href="/careers/v/%s">Job %s</a></div><div class="details">Engineering, Full Time Position, Any - Remote</div></div>`,
			slug, slug,
		)
	}

	page.WriteString("</div></body></html>")

	return page.String()
}

// TestPeopleForceFollowsPagesWithoutTheDisplayingMarker is a regression test.
//
// Pagination used to continue only while a "Displaying X - Y of Z in total"
// string could be scraped off the page. Nothing guarantees one is there, a small
// board omits it, a localised or restyled template moves it out of the <p>/<div>
// the scraper looks in, and its absence silently ended the crawl after page one.
// The source still reported a plausible non-zero count, so `health` marked it ok
// and the truncation was invisible.
func TestPeopleForceFollowsPagesWithoutTheDisplayingMarker(t *testing.T) {
	t.Parallel()

	transport := &peopleForceTransport{pages: map[string]string{
		"https://acme.peopleforce.io/careers":        peopleForceCareersPage("", "one", "two"),
		"https://acme.peopleforce.io/careers?page=2": peopleForceCareersPage("", "three"),
		// Page three is unmapped, so the stub answers 404, which is how a board
		// says there is no such page.
	}}

	postings, errs := drain(PeopleForce(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)

	// Three postings across two pages: the page-one truncation would report two.
	must.Len(t, 3, postings)
	test.Len(t, 3, transport.requests)

	for _, posting := range postings {
		test.Eq(t, "acme", posting.Company)
		test.Eq(t, "Any - Remote", posting.Location)
	}
}

// peopleForceCareersPageWithDetails renders one posting whose details line is
// supplied verbatim, so a test can exercise the shapes that line comes in.
func peopleForceCareersPageWithDetails(details string) string {
	return `<html><body><div id="results">` +
		`<div class="row"><div class="title"><a href="/careers/v/dev-42">Job</a></div>` +
		`<div class="details">` + details + `</div></div>` +
		`</div></body></html>`
}

// TestPeopleForceReadsTheDetailsItAlreadyParsed is a regression test.
//
// The details line is the board's "<department>, <employment type>, <location>"
// string. The adapter's own comment has documented that shape since it was
// written, and the implementation then split on commas, kept the last segment as
// the location, and discarded the other two — after having already parsed them
// into a string in memory.
func TestPeopleForceReadsTheDetailsItAlreadyParsed(t *testing.T) {
	t.Parallel()

	transport := &peopleForceTransport{pages: map[string]string{
		"https://acme.peopleforce.io/careers": peopleForceCareersPage("", "one"),
	}}

	postings, errs := drain(PeopleForce(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	// "Engineering, Full Time Position, Any - Remote"
	test.Eq(t, "Engineering", postings[0].Department)
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, postings[0].WorkplaceType)
	test.Eq(t, "one", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: peopleForcePlatform, Key: "acme"}, postings[0].Source)

	// The location is still the last segment, exactly as before.
	test.Eq(t, "Any - Remote", postings[0].Location)
}

func TestPeopleForceDetailShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		details        string
		location       string
		department     string
		employmentType internal.EmploymentType
		workplaceType  internal.WorkplaceType
	}{
		{
			name:           "the documented three-part line",
			details:        "Engineering, Full Time Position, Any - Remote",
			location:       "Any - Remote",
			department:     "Engineering",
			employmentType: internal.EmploymentTypeFullTime,
			workplaceType:  internal.WorkplaceTypeRemote,
		},
		{
			name:          "no employment type published",
			details:       "Engineering, Kyiv",
			location:      "Kyiv",
			workplaceType: internal.WorkplaceTypeUnknown,
			// Two segments are ambiguous: "Kyiv, Ukraine" is a location that
			// happens to contain a comma, and reading its city as a department
			// would file real postings under one that does not exist. So a
			// two-part line yields no department at all.
		},
		{
			name:           "two parts where the first is an employment type",
			details:        "Internship, Berlin",
			location:       "Berlin",
			employmentType: internal.EmploymentTypeInternship,
		},
		{
			name:     "location only",
			details:  "Warsaw",
			location: "Warsaw",
		},
		{
			name:           "office rather than remote",
			details:        "Finance, Part Time, Office",
			location:       "Office",
			department:     "Finance",
			employmentType: internal.EmploymentTypePartTime,
			workplaceType:  internal.WorkplaceTypeOnsite,
		},
		{
			name:       "a place name normalises to no workplace type",
			details:    "Design, Contract, Kyiv, Ukraine",
			location:   "Ukraine",
			department: "Design",
			// "Contract" is read as an employment type wherever it sits, because
			// a place name never normalises to one.
			employmentType: internal.EmploymentTypeContract,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := &peopleForceTransport{pages: map[string]string{
				"https://acme.peopleforce.io/careers": peopleForceCareersPageWithDetails(tt.details),
			}}

			postings, errs := drain(PeopleForce(t.Context(), &http.Client{Transport: transport}, "acme"))

			must.SliceEmpty(t, errs)
			must.Len(t, 1, postings)

			test.Eq(t, tt.location, postings[0].Location)
			test.Eq(t, tt.department, postings[0].Department)
			test.Eq(t, tt.employmentType, postings[0].EmploymentType)
			test.Eq(t, tt.workplaceType, postings[0].WorkplaceType)
		})
	}
}

// TestPeopleForceStopsAtAPageWithNoResultsContainer covers the other way a board
// answers a page number past its end: a 200 with no job list on it. Past page one
// that is the end of the board and must not be reported as a broken source.
func TestPeopleForceStopsAtAPageWithNoResultsContainer(t *testing.T) {
	t.Parallel()

	transport := &peopleForceTransport{pages: map[string]string{
		"https://acme.peopleforce.io/careers":        peopleForceCareersPage("", "one"),
		"https://acme.peopleforce.io/careers?page=2": `<html><body><p>Nothing here</p></body></html>`,
	}}

	postings, errs := drain(PeopleForce(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	test.Len(t, 2, transport.requests)
}

// TestPeopleForceReportsAFirstPageWithNoJobList is the other half of that rule:
// on page one there is no board to have reached the end of, so the source is
// broken and has to say so instead of reporting zero jobs.
func TestPeopleForceReportsAFirstPageWithNoJobList(t *testing.T) {
	t.Parallel()

	transport := &peopleForceTransport{pages: map[string]string{
		"https://acme.peopleforce.io/careers": `<html><body><p>Nothing here</p></body></html>`,
	}}

	postings, errs := drain(PeopleForce(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestPeopleForceStopsWhenPagingRepeatsPageOne guards the risk that following
// pagination links introduces: a careers page that answers any "?page=" with
// page one is common, and with no total to stop it the loop would run to the
// crawl deadline.
func TestPeopleForceStopsWhenPagingRepeatsPageOne(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(peopleForceCareersPage("", "one", "two"))

	postings, errs := drain(PeopleForce(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	must.Len(t, 2, postings)
}

// TestPeopleForceStopsAtItsReportedTotal keeps the marker useful: where a board
// does publish one, it still saves the request that discovers the end.
func TestPeopleForceStopsAtItsReportedTotal(t *testing.T) {
	t.Parallel()

	transport := &peopleForceTransport{pages: map[string]string{
		"https://acme.peopleforce.io/careers": peopleForceCareersPage(
			"<p>Displaying 1 - 2 of 2 in total</p>", "one", "two",
		),
		// Page two exists, so an adapter that ignored the total would fetch it.
		"https://acme.peopleforce.io/careers?page=2": peopleForceCareersPage("", "three"),
	}}

	postings, errs := drain(PeopleForce(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)
	test.Len(t, 1, transport.requests)
}

// TestPeopleForceStopsWhenTheConsumerDoes guards the iterator contract the
// health command depends on: it caps each source at 100 postings by returning
// false from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
func TestPeopleForceStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(peopleForceCareersPage("", "one", "two", "three"))

	var seen int

	for range PeopleForce(t.Context(), client, "acme") {
		seen++

		if seen == 2 {
			break
		}
	}

	test.Eq(t, 2, seen)
	test.Eq(t, 1, transport.requests)
}
