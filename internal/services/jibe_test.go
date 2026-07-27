package services

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
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

// TestJibeRecordsItsSourceIdentity covers the one enrichment this adapter got.
//
// The rest of Jibe's payload stays unmodelled on purpose: no live body has ever
// been decoded here beyond the four fields the adapter reads, and a guessed
// field *type* fails the decode and takes the tenant with it — which is exactly
// what modelling "meta_data" as a struct did to nine large employers. Source is
// different in kind: it comes from the registration rather than the response, so
// it needs no guess about what the board sends.
func TestJibeRecordsItsSourceIdentity(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"jibeapply.com": `{
			"jobs": [{"data": {
				"title": "Cloud Security Engineer",
				"apply_url": "https://acme.jibeapply.com/jobs/1",
				"full_location": "Chicago, IL"
			}}],
			"totalCount": 1,
			"meta_data": false
		}`,
	})

	postings, errs := drain(Jibe(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, internal.PostingSource{Platform: jibePlatform, Key: "acme"}, postings[0].Source)
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

// TestJibeHostAcceptsBothKeyForms is a regression test.
//
// This adapter only ever built "{key}.jibeapply.com", but iCIMS serves the
// identical /api/jobs endpoint from employers' own domains, so every board on a
// vanity host was invisible to the crawl despite the response shape already
// being modelled here. The .icims.com host is not an alternative: it 404s on
// /api/jobs.
func TestJibeHostAcceptsBothKeyForms(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  string
		host string
		name string
	}{
		{key: "fedex", host: "fedex.jibeapply.com", name: "fedex"},
		{key: "careers.costco.com", host: "careers.costco.com", name: "costco"},
		{key: "jobs.jcp.com", host: "jobs.jcp.com", name: "jcp"},
		{key: "careers.se.com", host: "careers.se.com", name: "se"},
		{key: "www.cakecareers.com", host: "www.cakecareers.com", name: "cakecareers"},
		{key: "aus.jibeapply.com", host: "aus.jibeapply.com", name: "aus"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			test.Eq(t, tc.host, jibeHost(tc.key))
			test.Eq(t, tc.name, jibeCompanyName(tc.key))
		})
	}
}

// TestJibeRequestsTheVanityHost proves the key reaches the wire, since the
// mapping above is only worth anything if the request actually goes there.
func TestJibeRequestsTheVanityHost(t *testing.T) {
	t.Parallel()

	var requested string

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requested = req.URL.Host

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"totalCount":0,"jobs":[]}`)),
			Request:    req,
		}, nil
	})}

	drain(Jibe(t.Context(), client, "careers.costco.com"))

	test.Eq(t, "careers.costco.com", requested)
}

// TestJibeVanityHostsAreNotDoubleRegistered guards the company list itself: a
// vanity host whose derived name duplicates an existing bare slug would crawl
// the same employer twice and report it twice in the company list.
func TestJibeVanityHostsAreNotDoubleRegistered(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}

	for _, key := range JibeCompanies {
		name := jibeCompanyName(key)

		if previous, ok := seen[name]; ok {
			t.Errorf("keys %q and %q both resolve to company %q", previous, key, name)
		}

		seen[name] = key
	}
}
