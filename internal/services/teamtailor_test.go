package services

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestTeamtailor(t *testing.T) {
	testSingle(t, "tibber", Teamtailor)
}

func TestTeamtailor_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	testMultipleParallel(t, slices.Values(TeamtailorCompanies), Teamtailor)
}

// candidateSlugs reads a staged candidate list from testdata, dropping comments
// and blank lines.
//
// The candidate files carry inline comments ("safetywing  # SafetyWing (~2)"),
// so the cut has to happen before the trim rather than only on whole-line
// comments.
func candidateSlugs(t *testing.T, name string) map[string]bool {
	t.Helper()

	file, err := os.Open(filepath.Join("testdata", "candidates", name))
	must.NoError(t, err)

	defer file.Close()

	entries := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")

		if line = strings.TrimSpace(line); line != "" {
			entries[line] = true
		}
	}

	must.NoError(t, scanner.Err())

	return entries
}

// teamtailorPageTransport serves one body per exact request URL.
//
// The shared fixtureTransport matches on a URL substring, and every page of a
// JSON Feed shares the substring "jobs.json", so a paginated fixture built on it
// would serve whichever route the map happened to iterate to first. Matching the
// whole URL keeps these tests deterministic.
type teamtailorPageTransport struct {
	pages    map[string]string
	requests []string
}

func (tr *teamtailorPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.requests = append(tr.requests, req.URL.String())

	body, ok := tr.pages[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     http.StatusText(http.StatusNotFound),
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"error":"no fixture"}`)),
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/feed+json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// TestTeamtailorParsesFeed covers the fields the JSON Feed and its embedded
// schema.org block carry, including the polymorphism schema.org allows: the
// second item writes its identifier as a bare number, its country as a node
// object and its jobLocation as a single Place rather than a list, all of which
// are as valid as the first item's shapes.
func TestTeamtailorParsesFeed(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.teamtailor.com/jobs.json": `{
			"version": "https://jsonfeed.org/version/1",
			"title": "Jobs at Acme",
			"items": [
				{
					"id": "6f1f2f5e-0000-4000-8000-000000000001",
					"url": "https://acme.teamtailor.com/jobs/123456-security-engineer",
					"title": "  Security Engineer  ",
					"date_published": "2026-04-30T16:21:55+02:00",
					"date_modified": "2026-05-02T09:00:00+02:00",
					"content_html": "<p>ignored</p>",
					"_jobposting": {
						"@type": "JobPosting",
						"identifier": {"@type": "PropertyValue", "name": "Acme", "value": "123456"},
						"employmentType": "FULL_TIME",
						"jobLocationType": "TELECOMMUTE",
						"occupationalCategory": "Engineering",
						"jobLocation": [
							{"address": {"addressLocality": "Stockholm", "addressCountry": "SE"}},
							{"address": {"addressLocality": "Oslo", "addressCountry": "NO"}}
						]
					}
				},
				{
					"id": "6f1f2f5e-0000-4000-8000-000000000002",
					"url": "https://acme.teamtailor.com/jobs/654321-support-intern",
					"title": "Support Intern",
					"date_published": "2026-05-04T08:00:00Z",
					"_jobposting": {
						"identifier": 654321,
						"employmentType": ["INTERN"],
						"jobLocation": {"address": {
							"addressLocality": "Berlin",
							"addressCountry": {"@type": "Country", "name": "DE"}
						}}
					}
				},
				{
					"id": "6f1f2f5e-0000-4000-8000-000000000003",
					"url": "https://acme.teamtailor.com/jobs/999999-untitled",
					"title": "   ",
					"_jobposting": {}
				}
			]
		}`,
	})

	postings, errs := drain(Teamtailor(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	remote := postings[0]

	test.Eq(t, "Security Engineer", remote.Title)
	test.Eq(t, "acme", remote.Company)
	test.Eq(t, "https://acme.teamtailor.com/jobs/123456-security-engineer", remote.URL)
	test.Eq(t, "Stockholm, SE; Oslo, NO; Remote", remote.Location)
	test.Eq(t, "Engineering", remote.Department)
	test.Eq(t, internal.EmploymentTypeFullTime, remote.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, remote.WorkplaceType)
	test.Eq(t, "123456", remote.ExternalID)
	test.Eq(t, internal.PostingSource{Platform: "teamtailor", Key: "acme"}, remote.Source)

	must.NotNil(t, remote.Remote)
	test.True(t, *remote.Remote)

	// Timestamps are stored in UTC, so the +02:00 the board rendered in is gone
	// and two boards' postings compare as instants.
	test.Eq(t, "2026-04-30T14:21:55Z", remote.PostedAt.Format(time.RFC3339))
	test.Eq(t, "2026-05-02T07:00:00Z", remote.UpdatedAt.Format(time.RFC3339))

	intern := postings[1]

	test.Eq(t, "654321", intern.ExternalID)
	test.Eq(t, "Berlin, DE", intern.Location)
	test.Eq(t, internal.EmploymentTypeInternship, intern.EmploymentType)

	// schema.org has no value meaning "office required", so an absent
	// jobLocationType leaves both remote fields empty rather than saying no.
	test.Nil(t, intern.Remote)
	test.Eq(t, internal.WorkplaceTypeUnknown, intern.WorkplaceType)
}

func TestTeamtailorReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"teamtailor.com": `{}`},
		status: http.StatusNotFound,
	}

	postings, errs := drain(Teamtailor(t.Context(), &http.Client{Transport: transport}, "gone"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "gone")
}

func TestTeamtailorReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"teamtailor.com": `{"items": [`})

	postings, errs := drain(Teamtailor(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestTeamtailorReportsAFeedWithNoItems covers the silently-empty failure this
// project treats as its worst: a 200 that is not the feed at all — a marketing
// page rendered as JSON, an error envelope — decodes into a struct whose items
// slice is empty, and reporting that as "this company is not hiring" would hide
// the break. The nil-versus-empty distinction is why the field is a pointer.
func TestTeamtailorReportsAFeedWithNoItems(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"teamtailor.com": `{"version": "https://jsonfeed.org/version/1", "title": "Acme"}`,
	})

	postings, errs := drain(Teamtailor(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "items")
}

// TestTeamtailorReportsEmptyBoardWithoutError is the other half of that
// distinction: an empty items array is a real answer from a company that is not
// hiring today, and docs/adding-a-source.md is explicit that is not a failure.
func TestTeamtailorReportsEmptyBoardWithoutError(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"teamtailor.com": `{"items": []}`})

	postings, errs := drain(Teamtailor(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

// TestTeamtailorReportsItemsThatYieldNoPostings covers a renamed field: the feed
// is full of items, and not one of them produces a posting. No live board does
// that, so it is reported rather than passed off as an empty company.
func TestTeamtailorReportsItemsThatYieldNoPostings(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"teamtailor.com": `{"items": [
			{"id": "a", "headline": "Security Engineer", "link": "https://acme.teamtailor.com/jobs/1"},
			{"id": "b", "headline": "Support Engineer", "link": "https://acme.teamtailor.com/jobs/2"}
		]}`,
	})

	postings, errs := drain(Teamtailor(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "2 feed items decoded")
}

// TestTeamtailorFollowsNextURL covers JSON Feed pagination: a feed that links to
// another page is followed, and the postings of both pages are returned.
func TestTeamtailorFollowsNextURL(t *testing.T) {
	t.Parallel()

	transport := &teamtailorPageTransport{pages: map[string]string{
		"https://acme.teamtailor.com/jobs.json": `{
			"items": [{"id": "1", "url": "https://acme.teamtailor.com/jobs/1-a", "title": "A"}],
			"next_url": "https://acme.teamtailor.com/jobs.json?page=2"
		}`,
		"https://acme.teamtailor.com/jobs.json?page=2": `{
			"items": [{"id": "2", "url": "https://acme.teamtailor.com/jobs/2-b", "title": "B"}]
		}`,
	}}

	postings, errs := drain(Teamtailor(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)
	test.Eq(t, "A", postings[0].Title)
	test.Eq(t, "B", postings[1].Title)
	test.Len(t, 2, transport.requests)
}

// TestTeamtailorRefusesToFollowNextURLOffHost is a safety bound, not a
// correctness one. next_url is a URL a third party chose and this adapter would
// otherwise fetch it with the project's client, under the project's User-Agent,
// and outside the limiter key the teamtailor.com hosts share. Silently stopping
// instead would truncate the board, so it is reported.
func TestTeamtailorRefusesToFollowNextURLOffHost(t *testing.T) {
	t.Parallel()

	transport := &teamtailorPageTransport{pages: map[string]string{
		"https://acme.teamtailor.com/jobs.json": `{
			"items": [{"id": "1", "url": "https://acme.teamtailor.com/jobs/1-a", "title": "A"}],
			"next_url": "https://example.invalid/jobs.json?page=2"
		}`,
	}}

	postings, errs := drain(Teamtailor(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.Len(t, 1, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "example.invalid")
	test.Len(t, 1, transport.requests)
}

// TestTeamtailorTreatsASelfReferentialNextURLAsTheEnd covers the cheaper half of
// the pagination bound. A feed whose next_url is the page it arrived on is
// finished, and recognising that costs nothing; [pageRepeatGuard] would stop the
// loop too, but only after spending a second request on every such tenant, every
// night.
func TestTeamtailorTreatsASelfReferentialNextURLAsTheEnd(t *testing.T) {
	t.Parallel()

	transport := &teamtailorPageTransport{pages: map[string]string{
		"https://acme.teamtailor.com/jobs.json": `{
			"items": [{"id": "1", "url": "https://acme.teamtailor.com/jobs/1-a", "title": "A"}],
			"next_url": "https://acme.teamtailor.com/jobs.json"
		}`,
	}}

	postings, errs := drain(Teamtailor(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	test.Len(t, 1, transport.requests)
}

// TestTeamtailorStopsWhenTheFeedRepeatsAPage is a regression test for the class
// of bug the repo has just finished repairing across eight adapters: a board
// that answers the next page with the page just served makes an unguarded loop
// run until the crawl deadline, pinning a worker and hammering one host, while
// internal.Dedupe hides the duplicates so the posting total looks unremarkable.
func TestTeamtailorStopsWhenTheFeedRepeatsAPage(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(`{
		"items": [{"id": "1", "url": "https://acme.teamtailor.com/jobs/1-a", "title": "A"}],
		"next_url": "https://acme.teamtailor.com/jobs.json?page=2"
	}`)

	postings, errs := drain(Teamtailor(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, 1, postings)
}

// teamtailorEndlessTransport answers every request with a page of new postings
// that links to yet another page, which no repeated-page check can catch.
type teamtailorEndlessTransport struct {
	requests int
}

func (tr *teamtailorEndlessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.requests++

	body := fmt.Sprintf(`{
		"items": [{"id": "%d", "url": "https://acme.teamtailor.com/jobs/%d-a", "title": "Job %d"}],
		"next_url": "https://acme.teamtailor.com/jobs.json?page=%d"
	}`, tr.requests, tr.requests, tr.requests, tr.requests+1)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/feed+json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// TestTeamtailorStopsAtItsPageCeiling covers the backstop for the case a
// repeated page cannot catch. Hitting the ceiling is reported rather than passed
// off as the end of a board.
func TestTeamtailorStopsAtItsPageCeiling(t *testing.T) {
	t.Parallel()

	transport := &teamtailorEndlessTransport{}

	postings, errs := drain(Teamtailor(t.Context(), &http.Client{Transport: transport}, "acme"))

	test.Eq(t, teamtailorMaxPages, transport.requests)
	test.Len(t, teamtailorMaxPages, postings)

	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "refusing to keep paginating")
}

// TestTeamtailorStopsWhenTheConsumerDoes guards the iterator contract the health
// command depends on: it caps each source by returning false from yield, and an
// adapter that fetches another page afterwards both burns the budget the cap
// exists to save and risks calling yield again, which panics.
func TestTeamtailorStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	transport := &teamtailorEndlessTransport{}

	for range Teamtailor(t.Context(), &http.Client{Transport: transport}, "acme") {
		break
	}

	test.Eq(t, 1, transport.requests)
}

// TestTeamtailorCompanyName covers the display name derived from a subdomain.
// The suffix is a unix timestamp from the account's creation, so it is stripped
// only when it is long enough to be one; a company whose name merely ends in a
// number keeps it.
func TestTeamtailorCompanyName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		slug string
		want string
	}{
		{"tibber", "tibber"},
		{"cameramatics-1639649453", "cameramatics"},
		{"felyx-1732008592", "felyx"},
		{"360t", "360t"},
		{"chapter-2", "chapter-2"},
		{"foothill.na", "foothill.na"},
		{"  tibber  ", "tibber"},
	} {
		test.Eq(t, tc.want, teamtailorCompanyName(tc.slug), test.Sprintf("slug %q", tc.slug))
	}
}

// TestTeamtailorCompaniesComeFromTheCandidateFile keeps the registered list
// honest: every slug in it is one a research pass actually recorded, and the
// registered set stays a small staged subset rather than the whole unprobed
// harvest. Registering all 1,037 candidates would put a thousand unverified
// sources into a crawl that already misses its deadline, and enough failing ones
// would trip the source-health alarm that is supposed to mean a real platform
// broke.
func TestTeamtailorCompaniesComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "teamtailor_slugs.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(TeamtailorCompanies))

	for _, slug := range TeamtailorCompanies {
		test.False(t, seen[slug], test.Sprintf("company %q is registered twice", slug))
		seen[slug] = true

		test.True(t, candidates[slug], test.Sprintf("registered company %q is not in testdata/candidates/teamtailor_slugs.txt", slug))
	}

	test.Less(t, len(candidates), len(TeamtailorCompanies), test.Sprint("the registered list should stay a subset of the candidates"))
}
