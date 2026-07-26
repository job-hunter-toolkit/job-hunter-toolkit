package services

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestSmartRecruiters(t *testing.T) {
	testSingle(t, "PaloAltoNetworks2", SmartRecruiters) // "McDonaldsCorporation", "visa"
}

// smartRecruitersPageBody builds a page of exactly 100 postings whose ids carry
// prefix, so a caller can make two pages either identical or distinct.
func smartRecruitersPageBody(prefix string, totalFound int) string {
	items := make([]string, 100)

	for i := range items {
		items[i] = fmt.Sprintf(
			`{"id":"%s%d","name":"Job %s%d","location":{"city":"Austin","region":"TX","country":"us"}}`,
			prefix, i, prefix, i)
	}

	return fmt.Sprintf(`{"totalFound":%d,"content":[%s]}`, totalFound, strings.Join(items, ","))
}

// TestSmartRecruitersStopsWhenTheTenantIgnoresOffset is a regression test.
//
// This adapter's only stop conditions were an empty page and the tenant's own
// "totalFound", both supplied by the server. A tenant that answers every offset
// with the same full page while reporting a large total therefore paginated
// until the crawl deadline, emitting duplicates that internal.Dedupe then hid,
// so the only visible symptom was a slow crawl. Every other paginating adapter
// in this package was given a bound after that failure was reproduced against
// it; this one was missed because nothing looks wrong while a server behaves.
func TestSmartRecruitersStopsWhenTheTenantIgnoresOffset(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(smartRecruitersPageBody("", 1_000_000))

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, 100, postings)
}

// smartRecruitersOffsetEchoTransport serves a full page whose posting ids embed
// the offset they were served at, so no two pages are ever alike and the page
// fingerprint cannot end the loop. Only a hard ceiling can.
type smartRecruitersOffsetEchoTransport struct {
	requests int
}

func (s *smartRecruitersOffsetEchoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests++

	if s.requests > smartRecruitersMaxPages*2 {
		return nil, fmt.Errorf("made %d requests to %s: pagination is unbounded", s.requests, req.URL)
	}

	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			smartRecruitersPageBody("o"+strconv.Itoa(offset)+"-", 1_000_000))),
		Request: req,
	}, nil
}

// TestSmartRecruitersBoundsPagesWhenEveryPageDiffers proves the backstop.
//
// A tenant reporting an enormous totalFound while serving a genuinely different
// page at every offset defeats the repeat fingerprint, which is why the ceiling
// exists. It must stop, and it must say why rather than truncating silently.
func TestSmartRecruitersBoundsPagesWhenEveryPageDiffers(t *testing.T) {
	t.Parallel()

	transport := &smartRecruitersOffsetEchoTransport{}
	client := &http.Client{Transport: transport}

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	test.Eq(t, smartRecruitersMaxPages, transport.requests)
	test.Len(t, smartRecruitersMaxPages*100, postings)

	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "refusing to keep paginating")
}

// TestSmartRecruitersRequestsTheOffsetItAdvancesTo guards the query this adapter
// builds, since the bound above is only meaningful if the offset really moves.
func TestSmartRecruitersRequestsTheOffsetItAdvancesTo(t *testing.T) {
	t.Parallel()

	var offsets []string

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		offsets = append(offsets, req.URL.Query().Get("offset"))

		body := `{"totalFound":150,"content":[]}`
		if len(offsets) == 1 {
			body = smartRecruitersPageBody("a", 150)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	test.Len(t, 100, postings)
	test.Eq(t, []string{"0", "100"}, offsets)

	// The posting URL is built from the tenant and the posting id, and the
	// tenant name is used verbatim, so a name needing escaping would break it.
	must.SliceNotEmpty(t, postings)
	test.Eq(t, "https://jobs.smartrecruiters.com/acme/a0", postings[0].URL)
	test.Eq(t, url.PathEscape("acme"), "acme")
}

// TestSmartRecruitersReadsTheDocumentedFieldSet covers the enrichment that
// rides in the list response this adapter already fetches: no extra request, no
// extra host, one longer struct.
func TestSmartRecruitersReadsTheDocumentedFieldSet(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"offset=0": `{"totalFound":2,"content":[
			{
				"id": "744000012345",
				"name": "Security Analyst",
				"refNumber": "REF-8891",
				"releasedDate": "2026-06-01T09:15:00.000Z",
				"location": {"city": "Austin", "region": "TX", "country": "us", "remote": true},
				"department": {"id": "d1", "label": "Information Security"},
				"function": {"id": "f1", "label": "IT"},
				"experienceLevel": {"id": "mid", "label": "Mid-Senior Level"},
				"typeOfEmployment": {"label": "Full-time"}
			},
			{
				"id": "744000067890",
				"name": "Store Manager",
				"releasedDate": "2026-05-20",
				"location": {"city": "Dallas", "region": "TX", "country": "us", "remote": false},
				"function": {"id": "f2", "label": "Retail"},
				"typeOfEmployment": {"label": "Permanent"}
			}
		]}`,
	})

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	test.Eq(t, "Information Security", postings[0].Department)
	test.Eq(t, "Mid-Senior Level", postings[0].Seniority)
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, "REF-8891", postings[0].RequisitionID)
	test.Eq(t, "744000012345", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: smartRecruitersPlatform, Key: "acme"}, postings[0].Source)
	test.Eq(t, time.Date(2026, time.June, 1, 9, 15, 0, 0, time.UTC), postings[0].PostedAt)

	must.NotNil(t, postings[0].Remote)
	test.True(t, *postings[0].Remote)
	test.Eq(t, internal.WorkplaceTypeRemote, postings[0].WorkplaceType)

	// "function" is the standardised job function rather than the employer's own
	// org unit, so it stands in only when there is no department. It is not
	// recorded as a Team, which would claim a hierarchy the board never
	// published.
	test.Eq(t, "Retail", postings[1].Department)
	test.Eq(t, "", postings[1].Team)

	// A bare date still resolves; "Permanent" is tenure rather than hours and is
	// deliberately not read as full time.
	test.Eq(t, time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC), postings[1].PostedAt)
	test.Eq(t, internal.EmploymentTypeUnknown, postings[1].EmploymentType)

	// remote=false is not an office requirement, so nothing is claimed.
	test.Nil(t, postings[1].Remote)
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[1].WorkplaceType)
}

// TestSmartRecruitersSurvivesUnexpectedFieldShapes is the guard that matters
// most about this change.
//
// Every enrichment field here comes from vendor documentation rather than from a
// body this project has decoded, and a field whose real type differs fails the
// whole decode — which is one tenant's entire posting list, gone, with an error
// nobody would connect to an enrichment commit. That is precisely what happened
// when Jibe's "meta_data" was modelled as a struct and turned out to be a bare
// `false` on some tenants: nine large employers silently disappeared.
//
// So: a tenant may send any of these in any shape at all, and the postings must
// still come through with their core fields intact.
func TestSmartRecruitersSurvivesUnexpectedFieldShapes(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"offset=0": `{"totalFound":1,"content":[{
			"id": "1",
			"name": "Security Analyst",
			"refNumber": 8891,
			"releasedDate": 1780000000,
			"location": {"city": "Austin", "region": "TX", "country": "us", "remote": "true"},
			"department": "Information Security",
			"function": ["IT"],
			"experienceLevel": null,
			"typeOfEmployment": {"label": ["Full-time"]}
		}]}`,
	})

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, "Security Analyst", postings[0].Title)
	test.Eq(t, "https://jobs.smartrecruiters.com/acme/1", postings[0].URL)

	// A bare string where an object was documented is still a usable label.
	test.Eq(t, "Information Security", postings[0].Department)

	// A number where a string was documented still reads.
	test.Eq(t, "8891", postings[0].RequisitionID)

	// A remote flag sent as a string is still an answer.
	must.NotNil(t, postings[0].Remote)
	test.True(t, *postings[0].Remote)

	// Shapes with nothing readable in them leave the field empty, which is what
	// every consumer already treats as "the board did not say".
	test.Eq(t, "", postings[0].Seniority)
	test.Eq(t, internal.EmploymentTypeUnknown, postings[0].EmploymentType)
	test.True(t, postings[0].PostedAt.IsZero())
}

// TestSmartRecruitersAsksForAFullPage guards the page size.
//
// This request sent no "limit" at all, so every tenant was paged at the API's
// default of ten. A 900-posting employer cost 90 requests against
// api.smartrecruiters.com — a host shared by all 54 tenants — where 9 would do.
func TestSmartRecruitersAsksForAFullPage(t *testing.T) {
	t.Parallel()

	var limits []string

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		limits = append(limits, req.URL.Query().Get("limit"))

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"totalFound":0,"content":[]}`)),
			Request:    req,
		}, nil
	})}

	_, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Eq(t, []string{strconv.Itoa(smartRecruitersPageSize)}, limits)
	test.Eq(t, 100, smartRecruitersPageSize, test.Sprintf("100 is the API's documented maximum"))
}

// roundTripFunc adapts a function to [net/http.RoundTripper].
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
