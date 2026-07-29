package services

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// avatureFixture reads a search page captured from a live Avature career site.
//
// The six captures under testdata are byte-for-byte what
// https://{host}{section}/SearchJobs/?jobOffset={n} returned on 2026-07-29,
// with nothing removed. They are whole pages rather than trimmed rows because
// what varies between career sites is the page: which of five list templates
// the tenant uses, whether the pagination links are marked "next", and whether
// the metadata spans carry semantic classes are all page-level facts, and a
// hand-trimmed fixture is where they would be lost.
//
// Between them the six cover every list template measured across the 87 sites:
//
//	ally         <article class="article article--result"> with unclassed
//	             metadata spans separated by bare punctuation
//	forvis       the same article, with list-item-location / -ref / -posted
//	xerox        the same article, with labelled <p>City:</p> paragraphs and no
//	             posted date at all
//	landolakes   a legacy <div class="jobItem"> with locationText / daysAgo
//	cuboulder    <li class="listSingleColumnItem"> on a vanity host that is not
//	             under avature.net
//	ally offset66 the past-the-end page, which is the reason this adapter cannot
//	             trust a Next link
func avatureFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// avatureCollect walks one career site against a fixed set of pages and returns
// the postings, failing the test on any error.
func avatureCollect(t *testing.T, key string, pages map[string]string) []*internal.JobPosting {
	t.Helper()

	transport := &icimsPageTransport{pages: pages}
	client := &http.Client{Transport: transport}

	var postings []*internal.JobPosting

	for posting, err := range Avature(t.Context(), client, key) {
		must.NoError(t, err)

		postings = append(postings, posting)
	}

	return postings
}

// avatureSinglePageBoard serves one captured page as a complete career site, by
// answering every other offset with the page a real site returns past its end.
//
// Without it a capture of offset 0 advertises offset 6, the fixture map does not
// hold it, and the walk ends on an HTTP 404 error rather than on the site
// running out. Every test below that only cares about parsing would then be
// asserting on an error path.
func avatureSinglePageBoard(t *testing.T, host, section, page string) map[string]string {
	t.Helper()

	pages := map[string]string{
		avatureSearchURL(host, section, 0): page,
	}

	empty := avatureFixture(t, "avature_ally_search_offset66.html")

	for offset := 1; offset <= 200; offset++ {
		pages[avatureSearchURL(host, section, offset)] = empty
	}

	return pages
}

func TestAvatureParsesTheArticleSkin(t *testing.T) {
	t.Parallel()

	const (
		key  = "ally,ally.avature.net,/careers"
		host = "ally.avature.net"
	)

	postings := avatureCollect(t, key, avatureSinglePageBoard(t, host, "/careers",
		avatureFixture(t, "avature_ally_search_offset0.html")))

	must.Len(t, 6, postings)

	first := postings[0]

	test.Eq(t, "https://ally.avature.net/careers/JobDetail/Manager-Invest-Fraud-Strategy-and-Operations/17055", first.URL)
	test.Eq(t, "Manager, Invest Fraud Strategy and Operations", first.Title)
	test.Eq(t, "ally", first.Company)
	test.Eq(t, "17055", first.ExternalID)
	test.Eq(t, avaturePlatform, first.Source.Platform)
	test.Eq(t, key, first.Source.Key)

	// The row writes "Charlotte , NC , USA , Ref #22821 . Posted Jul-28-2026"
	// as five unclassed spans. Three of them are the location, and the two that
	// name themselves are not.
	test.Eq(t, "Charlotte, NC, USA", first.Location)
	test.Eq(t, "22821", first.RequisitionID)
	test.Eq(t, time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC), first.PostedAt)

	// Ally's requisition number is not its posting id, which is exactly why one
	// is RequisitionID and the other ExternalID.
	test.NotEq(t, first.ExternalID, first.RequisitionID)

	for _, posting := range postings {
		test.StrHasPrefix(t, "https://ally.avature.net/careers/JobDetail/", posting.URL)
		test.StrNotContains(t, posting.URL, "?")
	}
}

func TestAvatureParsesTheClassedSkin(t *testing.T) {
	t.Parallel()

	postings := avatureCollect(t, "forvis,forvis.avature.net,/campuscareers",
		avatureSinglePageBoard(t, "forvis.avature.net", "/campuscareers",
			avatureFixture(t, "avature_forvis_search_offset0.html")))

	must.Len(t, 6, postings)

	first := postings[0]

	test.Eq(t, "https://forvis.avature.net/campuscareers/JobDetail/Atlanta-Georgia-United-States-Associate-Audit-Fall-2027-Atlanta/13268", first.URL)
	test.Eq(t, "Associate Audit Fall 2027 | Atlanta", first.Title)
	test.Eq(t, "Atlanta, Georgia, United States", first.Location)
	test.Eq(t, "2235322", first.RequisitionID)
	test.Eq(t, time.Date(2025, time.July, 23, 0, 0, 0, 0, time.UTC), first.PostedAt)
}

func TestAvatureParsesLabelledLocationParagraphs(t *testing.T) {
	t.Parallel()

	postings := avatureCollect(t, "xerox,xerox.avature.net,/en_US/careers",
		avatureSinglePageBoard(t, "xerox.avature.net", "/en_US/careers",
			avatureFixture(t, "avature_xerox_search_offset0.html")))

	must.Len(t, 5, postings)

	first := postings[0]

	test.Eq(t, "https://xerox.avature.net/en_US/careers/JobDetail/2L-Operations-Manager/51377", first.URL)
	test.Eq(t, "Cebu City, Central Visayas (Region VII), Philippines", first.Location)
	test.Eq(t, "20040269", first.RequisitionID)

	// Xerox publishes no date on the list at all, and a posting with no date is
	// the correct outcome rather than one dated today.
	test.True(t, first.PostedAt.IsZero())
}

func TestAvatureParsesTheLegacySkin(t *testing.T) {
	t.Parallel()

	postings := avatureCollect(t, "lol,lol.avature.net,/careers",
		avatureSinglePageBoard(t, "lol.avature.net", "/careers",
			avatureFixture(t, "avature_landolakes_search_offset0.html")))

	must.Len(t, 10, postings)

	first := postings[0]

	test.Eq(t, "Uganda", first.Location)
	test.Eq(t, time.Date(2025, time.July, 10, 0, 0, 0, 0, time.UTC), first.PostedAt)

	// The legacy skin links its postings under /Careers, capitalised, while the
	// registered section is /careers. The canonical URL is the site's own, so
	// the case the site wrote is the case that is published.
	test.StrHasPrefix(t, "https://lol.avature.net/Careers/JobDetail/", first.URL)
}

func TestAvatureParsesAVanityHostListSkin(t *testing.T) {
	t.Parallel()

	postings := avatureCollect(t, "colorado,jobs.colorado.edu,/jobs",
		avatureSinglePageBoard(t, "jobs.colorado.edu", "/jobs",
			avatureFixture(t, "avature_cuboulder_search_offset0.html")))

	must.Len(t, 25, postings)

	first := postings[0]

	test.Eq(t, "https://jobs.colorado.edu/jobs/JobDetail/Communications-and-Marketing-Manager/73770", first.URL)
	test.Eq(t, "Communications and Marketing Manager", first.Title)

	// This template publishes its metadata in an unclassed, unlabelled
	// listSingleColumnItemMiscData block whose items are, in order, the
	// department, the requisition number, the location, the appointment type,
	// the schedule, a closing date and a posted date. Nothing in the markup says
	// which is which, so nothing is published: the ungated positional read this
	// adapter used to do turned all seven into one Location string, which is a
	// field that silently matches the wrong searches. "unknown" is a fact a
	// filter can act on.
	test.Eq(t, "unknown", first.Location)
	test.Eq(t, "", first.RequisitionID)
	test.True(t, first.PostedAt.IsZero())
}

// TestAvatureStopsAtAPastTheEndOffset is the regression test this platform most
// needs.
//
// A past-the-end offset answers HTTP 200 with a "No jobs found" page that still
// carries a Next link, and that link points at offset 6 -- backwards. Measured
// on ally.avature.net at offsets 66, 72 and 600 on 2026-07-29. An adapter that
// followed the site's own Next link would bounce between offset 6 and the end
// of the board until the crawl deadline, which is the shape that once ran 5,001
// requests against one host in this codebase.
func TestAvatureStopsAtAPastTheEndOffset(t *testing.T) {
	t.Parallel()

	const (
		host    = "ally.avature.net"
		section = "/careers"
	)

	transport := &icimsPageTransport{pages: map[string]string{
		avatureSearchURL(host, section, 0): avatureFixture(t, "avature_ally_search_offset0.html"),
		avatureSearchURL(host, section, 6): avatureFixture(t, "avature_ally_search_offset66.html"),
	}}

	client := &http.Client{Transport: transport}

	var count int

	for _, err := range Avature(t.Context(), client, "ally,"+host+","+section) {
		must.NoError(t, err)

		count++
	}

	test.Eq(t, 6, count)
	test.Len(t, 2, transport.requests,
		test.Sprint("the walk must stop on the empty page rather than follow its backwards Next link"))
}

// TestAvatureStopsWhenACareerSiteIgnoresItsOffset covers the other half of the
// termination contract: a site that answers every offset with page one.
func TestAvatureStopsWhenACareerSiteIgnoresItsOffset(t *testing.T) {
	t.Parallel()

	const (
		host    = "ally.avature.net"
		section = "/careers"
	)

	page := avatureFixture(t, "avature_ally_search_offset0.html")
	pages := make(map[string]string, avatureMaxPages)

	for offset := 0; offset <= avatureMaxPages*10; offset += 6 {
		pages[avatureSearchURL(host, section, offset)] = page
	}

	transport := &icimsPageTransport{pages: pages}
	client := &http.Client{Transport: transport}

	var count int

	for _, err := range Avature(t.Context(), client, "ally,"+host+","+section) {
		must.NoError(t, err)

		count++
	}

	// The second request returns the first page's ids again, so pageRepeatGuard
	// ends the walk there and no posting is yielded twice.
	test.Eq(t, 6, count)
	test.Len(t, 2, transport.requests)
}

// TestAvatureReportsACareerSiteLargerThanThePagingWindow covers the platform's
// most dangerous behaviour: past 2,000 records it answers HTTP 200 with an
// error page that is indistinguishable from the end of a board.
//
// Four of the 87 sites probed on 2026-07-29 are already past it. Reporting the
// partial total is what this test exists to prevent, because the partial total
// looks exactly like a healthy one.
func TestAvatureReportsACareerSiteLargerThanThePagingWindow(t *testing.T) {
	t.Parallel()

	const (
		host    = "nva.avature.net"
		section = "/careers"
	)

	full := avatureFixture(t, "avature_ally_search_offset0.html")
	full = strings.ReplaceAll(full, "ally.avature.net", host)

	// Each page has to carry different postings or pageRepeatGuard ends the
	// walk on the second request, which is the guard working correctly and not
	// the state under test.
	pages := map[string]string{}
	for offset := 0; offset <= avatureResultWindow; offset += 6 {
		pages[avatureSearchURL(host, section, offset)] = strings.ReplaceAll(full,
			"/JobDetail/", "/JobDetail/"+strconv.Itoa(offset)+"-")
	}

	// What the site answers past the window: HTTP 200, no postings, no
	// pagination. Every offset beyond the window gets it.
	pages[avatureSearchURL(host, section, avatureResultWindow+4)] =
		`<html><body><h1>Oops&hellip; Something went wrong</h1></body></html>`

	client := &http.Client{Transport: &icimsPageTransport{pages: pages}}

	var (
		count int
		errs  []error
	)

	for posting, err := range Avature(t.Context(), client, "nva,"+host+","+section) {
		if err != nil {
			errs = append(errs, err)

			continue
		}

		_ = posting
		count++
	}

	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "cannot be crawled to the end")
	test.Greater(t, 0, count)
}

func TestAvatureDropsABotWalledCareerSite(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Status:     http.StatusText(http.StatusAccepted),
			Header:     http.Header{},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}

	var (
		count int
		errs  int
	)

	for _, err := range Avature(t.Context(), client, "ally,ally.avature.net,/careers") {
		if err != nil {
			errs++

			continue
		}

		count++
	}

	// A bot wall is a career site this project cannot read, which is not the
	// same event as a career site that broke. Reporting it as a failure would
	// put a permanent error floor into every crawl summary.
	test.Eq(t, 0, count)
	test.Eq(t, 0, errs)
}

func TestAvatureReportsACareerSiteThatMoved(t *testing.T) {
	t.Parallel()

	const moved = `<html><body><article class="article article--result">` +
		`<h3><a href="https://jobs.example.com/careers/JobDetail/Some-Role/1234">Some Role</a></h3>` +
		`</article></body></html>`

	client := &http.Client{Transport: &icimsPageTransport{pages: map[string]string{
		avatureSearchURL("ally.avature.net", "/careers", 0): moved,
	}}}

	var errs []error

	for _, err := range Avature(t.Context(), client, "ally,ally.avature.net,/careers") {
		if err != nil {
			errs = append(errs, err)
		}
	}

	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "jobs.example.com")
	test.StrContains(t, errs[0].Error(), "has moved")
}

func TestAvatureCanonicalURL(t *testing.T) {
	t.Parallel()

	const host = "ally.avature.net"

	for name, tc := range map[string]struct {
		href      string
		canonical string
		other     string
		ok        bool
	}{
		"absolute posting": {
			href:      "https://ally.avature.net/careers/JobDetail/Field-Auditor-Houston/17122",
			canonical: "https://ally.avature.net/careers/JobDetail/Field-Auditor-Houston/17122",
			ok:        true,
		},
		"relative posting": {
			href:      "/careers/JobDetail/Field-Auditor-Houston/17122",
			canonical: "https://ally.avature.net/careers/JobDetail/Field-Auditor-Houston/17122",
			ok:        true,
		},
		"query stripped": {
			href:      "https://ally.avature.net/careers/JobDetail/Role/13924?businessTitle=Role+13924",
			canonical: "https://ally.avature.net/careers/JobDetail/Role/13924",
			ok:        true,
		},
		"mailto share link": {
			href: "mailto:?body=https://ally.avature.net/careers/JobDetail/Field-Auditor-Houston/17122",
		},
		"third party share link": {
			href: "https://www.linkedin.com/shareArticle?url=https%3A%2F%2Fally.avature.net%2Fcareers%2FJobDetail%2FRole%2F17122",
		},
		"apply link": {
			href: "https://ally.avature.net/careers/ApplicationMethods?jobId=17055",
		},
		"posting on another host": {
			href:  "https://jobs.ea.com/en_US/careers/JobDetail/Role/9",
			other: "jobs.ea.com",
		},
		"non numeric id": {
			href: "https://ally.avature.net/careers/JobDetail/Role/apply",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			canonical, other, ok := avatureCanonicalURL(host, tc.href)

			test.Eq(t, tc.ok, ok)
			test.Eq(t, tc.canonical, canonical)
			test.Eq(t, tc.other, other)
		})
	}
}

func TestAvatureTime(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		value string
		want  time.Time
	}{
		"day first":      {value: "Posted 23-Jul-2025", want: time.Date(2025, time.July, 23, 0, 0, 0, 0, time.UTC)},
		"month first":    {value: "Posted Jul-28-2026", want: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)},
		"legacy colon":   {value: "Posted: 10-Jul-2025", want: time.Date(2025, time.July, 10, 0, 0, 0, 0, time.UTC)},
		"padded day":     {value: "Posted 02-Jan-2026", want: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)},
		"relative prose": {value: "Posted 3 days ago"},
		"empty":          {value: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tc.want, avatureTime(tc.value))
		})
	}
}

func TestAvatureRequisitionID(t *testing.T) {
	t.Parallel()

	test.Eq(t, "22821", avatureRequisitionID("Ref #22821"))
	test.Eq(t, "13924", avatureRequisitionID("Job ID 13924"))
	test.Eq(t, "2235322", avatureRequisitionID("Ref #2235322"))
	test.Eq(t, "R69556", avatureRequisitionID("Req # R69556"))
	test.Eq(t, "", avatureRequisitionID(""))
}

func TestAvatureSiteKey(t *testing.T) {
	t.Parallel()

	slug, host, section, ok := avatureSite("colorado,jobs.colorado.edu,/jobs")

	test.True(t, ok)
	test.Eq(t, "colorado", slug)
	test.Eq(t, "jobs.colorado.edu", host)
	test.Eq(t, "/jobs", section)

	_, _, _, ok = avatureSite("ally.avature.net")
	test.False(t, ok)

	test.Eq(t, "colorado", avatureCompanyName("colorado,jobs.colorado.edu,/jobs"))
}

func TestAvatureRejectsAMalformedKey(t *testing.T) {
	t.Parallel()

	var errs []error

	for _, err := range Avature(t.Context(), http.DefaultClient, "ally.avature.net") {
		if err != nil {
			errs = append(errs, err)
		}
	}

	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "slug,host,section")
}

// TestAvatureRegistryIsWellFormed keeps the triple list reviewable and keeps
// every registered host on one limiter key.
func TestAvatureRegistryIsWellFormed(t *testing.T) {
	t.Parallel()

	test.True(t, slices.IsSorted(AvatureCareerSites), test.Sprint("career site list is not sorted"))

	seenKey := make(map[string]bool, len(AvatureCareerSites))
	seenSlug := make(map[string]bool, len(AvatureCareerSites))

	var first httpx.ServicePolicy

	for i, site := range AvatureCareerSites {
		slug, host, section, ok := avatureSite(site)

		must.True(t, ok, must.Sprintf("%q is not a slug,host,section triple", site))
		test.False(t, seenKey[site], test.Sprintf("duplicate career site %q", site))
		test.False(t, seenSlug[slug], test.Sprintf("duplicate display name %q", slug))
		test.True(t, section == "" || strings.HasPrefix(section, "/"),
			test.Sprintf("section %q of %q must be a path", section, site))

		seenKey[site] = true
		seenSlug[slug] = true

		policy := httpx.ServicePolicyForHost(host, httpx.DefaultPerHostLimit)

		if i == 0 {
			first = policy
		}

		test.Eq(t, first.Key, policy.Key,
			test.Sprintf("%s got limiter key %q, not the shared %q: every Avature host is one vendor backend",
				host, policy.Key, first.Key))
	}
}

// TestAvatureLive walks every registered career site against the real service.
func TestAvatureLive(t *testing.T) {
	t.Parallel()

	testMultipleParallel(t, slices.Values(AvatureCareerSites), Avature)
}
