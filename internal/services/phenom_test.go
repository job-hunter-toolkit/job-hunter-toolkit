package services

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
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

// TestPhenomReadsTheRestOfTheEmbeddedPayload covers the fields that ride in the
// same embedded search blob the adapter already downloads and parses. Reading
// them costs no request, no byte and no new host.
func TestPhenomReadsTheRestOfTheEmbeddedPayload(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.example.com": phenomFixturePage(strings.Join([]string{
			`{"title":"Security Engineer","cityState":"Dallas, TX","jobId":"R12345",
			  "applyUrl":"https://acme.example.com/apply/1",
			  "postedDate":"2026-06-01","type":"Full Time","category":"Information Technology"}`,
			`{"title":"Summer Analyst","cityState":"Dallas, TX","jobId":"R12346",
			  "applyUrl":"https://acme.example.com/apply/2",
			  "postedDate":"2026-05-15T08:00:00Z","type":"Internship","category":"Finance"}`,
			`{"title":"Contracts Manager","cityState":"Dallas, TX","jobId":"R12347",
			  "applyUrl":"https://acme.example.com/apply/3",
			  "postedDate":"June 1st","type":"Regular","category":"Legal"}`,
		}, ",")),
	})

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	test.Eq(t, "Information Technology", postings[0].Department)
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC), postings[0].PostedAt)
	test.Eq(t, "R12345", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: phenomPlatform, Key: "acme.example.com"}, postings[0].Source)

	test.Eq(t, internal.EmploymentTypeInternship, postings[1].EmploymentType)
	test.Eq(t, time.Date(2026, time.May, 15, 8, 0, 0, 0, time.UTC), postings[1].PostedAt)

	// An unreadable date and a tenure word are both left empty rather than
	// guessed at. "Regular" says nothing about hours, and a date this project
	// invented would reach Filter.PostedSince with nobody able to notice.
	test.True(t, postings[2].PostedAt.IsZero())
	test.Eq(t, internal.EmploymentTypeUnknown, postings[2].EmploymentType)
	test.Eq(t, "Legal", postings[2].Department)
}

// TestPhenomPostedAtReadsTheSpellingsTenantsActuallyPublish pins the timestamp
// format every registered tenant was observed serving.
//
// This is a regression test for a silent one: [phenomDateLayouts] began with
// only time.RFC3339, which rejects a zone offset written without its colon.
// Every tenant in [PhenomCompanies] publishes exactly that — "+0000" — so the
// platform's entire PostedAt column was empty, and `--posted-since` quietly
// excluded every Phenom posting instead of filtering it. Nothing failed, no
// error was logged, and the count of postings was unaffected, which is why it
// survived until someone decoded a live body.
func TestPhenomPostedAtReadsTheSpellingsTenantsActuallyPublish(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   string
		want time.Time
	}{
		// Observed live. The colonless offset is the common case: it is what a
		// Phenom site fed by a Workday back end emits.
		{"colonless offset, milliseconds", "2026-07-16T00:00:00.000+0000", time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC)},
		{"colonless offset, wall clock", "2026-07-20T15:54:35.731+0000", time.Date(2026, time.July, 20, 15, 54, 35, 731_000_000, time.UTC)},
		{"colonless offset, no fraction", "2026-07-20T09:09:28+0000", time.Date(2026, time.July, 20, 9, 9, 28, 0, time.UTC)},

		// A non-UTC colonless offset must still normalise to UTC rather than
		// being read as if the digits were already UTC.
		{"colonless offset, not UTC", "2026-07-20T09:00:00-0500", time.Date(2026, time.July, 20, 14, 0, 0, 0, time.UTC)},

		// Still accepted, so adding the layout above widened the set rather
		// than replacing it.
		{"rfc3339", "2026-05-15T08:00:00Z", time.Date(2026, time.May, 15, 8, 0, 0, 0, time.UTC)},
		{"date only", "2026-06-01", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			posted, ok := phenomPostedAt(testCase.in)

			must.True(t, ok)
			test.Eq(t, testCase.want, posted)
		})
	}

	// An ambiguous or unreadable date stays unreadable. Guessing puts a wrong
	// date somewhere nothing downstream can notice it.
	for _, unreadable := range []string{"", "June 1st", "03/04/2026"} {
		if _, ok := phenomPostedAt(unreadable); ok {
			t.Errorf("phenomPostedAt(%q) parsed a date, want none", unreadable)
		}
	}
}

// TestPhenomSurvivesUnexpectedFieldShapes is the guard on the risk this
// enrichment takes.
//
// Live bodies from every tenant in [PhenomCompanies] have since been decoded,
// and postedDate, type and category were strings in all of them. The `any` is
// kept anyway because Phenom is a per-tenant template rather than one shared
// API: a wrong field *name* costs an empty column, but a wrong field *type*
// fails the page decode, which is the whole tenant. Modelling them as `any` is
// what makes the second outcome impossible — the same failure that took out
// nine large Jibe employers when "meta_data" turned out to be a bare `false`.
func TestPhenomSurvivesUnexpectedFieldShapes(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.example.com": phenomFixturePage(
			`{"title":"Security Engineer","cityState":"Dallas, TX","jobId":"R1",
			  "applyUrl":"https://acme.example.com/apply/1",
			  "postedDate":1780000000,"type":{"label":"Full Time"},
			  "category":["Information Technology","Security"]}`,
		),
	})

	postings, errs := drain(Phenom(t.Context(), client, "acme.example.com"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, "Security Engineer", postings[0].Title)

	// A list where a string was expected: the first element is what the
	// single-valued form of the same field means.
	test.Eq(t, "Information Technology", postings[0].Department)

	// Shapes with nothing readable in them leave the field empty.
	test.Eq(t, internal.EmploymentTypeUnknown, postings[0].EmploymentType)
	test.True(t, postings[0].PostedAt.IsZero())
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

// TestPhenomYieldsTheTenantsOwnURLNotTheApplyURL pins the fix for the largest
// dedupe defect this project has measured.
//
// A Phenom career site is a front end onto another ATS, and "applyUrl" points at
// that other system. Reading the first page of all 14 registered tenants on
// 2026-07-28: 9 published a Workday URL, 2 SuccessFactors, 1 Taleo, and only 2
// published no applyUrl at all. [internal.Dedupe] keys on URL, and a Phenom
// applyUrl onto Workday is the Workday posting URL with "/apply" appended, so
// the two routes to one opening never collapsed -- 5,103 postings on Lowe's and
// a further 1,556 on KBR, whose Workday tenant is registered right now.
//
// The two cases below are exactly those two shapes: a posting whose applyUrl is
// a foreign ATS must not carry it, and a posting with no applyUrl at all must
// still get a link.
func TestPhenomYieldsTheTenantsOwnURLNotTheApplyURL(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"careers.acme.com": phenomFixturePage(strings.Join([]string{
			// The Lowe's and KBR shape: applyUrl is a Workday posting URL with
			// "/apply" on the end, for a tenant this project also crawls
			// through Workday.
			`{"title":"HVAC Technician","cityState":"Dallas, TX","jobId":"R2123021",
			  "applyUrl":"https://acme.wd5.myworkdayjobs.com/Acme_Careers/job/Dallas-TX/HVAC-Technician_R2123021/apply"}`,
			// A tenant that takes applications on the Phenom site itself
			// publishes no applyUrl. It must still carry a link.
			`{"title":"Detection Engineer","cityState":"Remote","jobId":"R2123022","applyUrl":""}`,
		}, ",")),
	})

	postings, errs := drain(Phenom(t.Context(), client, "careers.acme.com"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	test.Eq(t, "https://careers.acme.com/us/en/job/R2123021", postings[0].URL)
	test.Eq(t, "https://careers.acme.com/us/en/job/R2123022", postings[1].URL)

	for _, posting := range postings {
		test.StrNotContains(t, posting.URL, "myworkdayjobs.com",
			test.Sprintf("posting %q carries another ATS's URL; its Source says phenom and "+
				"Dedupe keys on URL, so the two routes to this opening would both be counted", posting.Title))
		test.StrNotHasSuffix(t, "/apply", posting.URL)
	}
}

// TestPhenomFallsBackToApplyURLWithoutAJobID keeps the change above from
// deleting postings.
//
// Every tenant published a jobId on every row of a 100-row page when this was
// measured, so the fallback is not expected to fire. It exists because this
// project's contract is that a posting always carries a URL a person can open,
// and a tenant that ever stops sending jobId should lose the canonical link
// rather than the posting.
func TestPhenomFallsBackToApplyURLWithoutAJobID(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"careers.acme.com": phenomFixturePage(
			`{"title":"Field Engineer","cityState":"Reston, VA","jobId":"",
			  "applyUrl":"https://career4.successfactors.com/career?company=Acme&career_job_req_id=292628"}`,
		),
	})

	postings, errs := drain(Phenom(t.Context(), client, "careers.acme.com"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, "https://career4.successfactors.com/career?company=Acme&career_job_req_id=292628", postings[0].URL)
}
