package services

import (
	"encoding/json"
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

// oracleCloudTestTenant is the triple the fixtures below are served for. Its
// site number is a bare "CX_1"; the named-site form ("AEO-Careers",
// "PenskeCareers") is covered by TestOracleCloudBuildsTheFinder.
const oracleCloudTestTenant = "acme,eluq.fa.us2.oraclecloud.com,CX_1"

// oracleCloudListFixture is a requisition list in the shape
// recruitingCEJobRequisitions returns: everything hangs off a single-element
// "items" array, with the total and the requisitions as siblings inside it.
//
// It is built from the documented response shape rather than captured live,
// because this project's containers cannot reach a job board. The second
// requisition sends its Id as a JSON number and omits every enrichment field,
// which is the shape variation that has historically broken adapters here.
const oracleCloudListFixture = `{
	"items": [
		{
			"TotalJobsCount": 2,
			"requisitionList": [
				{
					"Id": "18234",
					"Title": "  Pharmacy Technician  ",
					"PrimaryLocation": "  Cincinnati, OH  ",
					"PostedDate": "2026-06-14",
					"JobType": "Full time",
					"WorkplaceTypeCode": "ORA_HYBRID",
					"JobFunction": "Pharmacy"
				},
				{
					"Id": 18235,
					"Title": "Store Associate",
					"PrimaryLocation": ""
				}
			]
		}
	]
}`

func TestOracleCloudParsesRequisitions(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"eluq.fa.us2.oraclecloud.com": oracleCloudListFixture,
	})

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// The list response already carries everything this project stores, so the
	// per-posting detail request every other implementation of this API makes is
	// skipped entirely: two postings, one request.
	must.Len(t, 1, transport.requests)

	technician, associate := postings[0], postings[1]

	test.Eq(t, "acme", technician.Company)
	test.Eq(t, "Pharmacy Technician", technician.Title)
	test.Eq(t, "Cincinnati, OH", technician.Location)
	test.Eq(t, "Pharmacy", technician.Department)
	test.Eq(t, internal.EmploymentTypeFullTime, technician.EmploymentType)

	// ORA_HYBRID is Oracle's genuine three-state workplace field, which a
	// Remote *bool could not have expressed at all.
	test.Eq(t, internal.WorkplaceTypeHybrid, technician.WorkplaceType)

	test.Eq(t, time.Date(2026, time.June, 14, 0, 0, 0, 0, time.UTC), technician.PostedAt)
	test.Eq(t, "UTC", technician.PostedAt.Location().String())

	test.Eq(t, "18234", technician.ExternalID)

	// The employer's own requisition number is a different field on this
	// platform and is not corroborated as present in the list, so it stays
	// empty rather than being filled with the ATS's id.
	test.Eq(t, "", technician.RequisitionID)

	test.Eq(t,
		"https://eluq.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/18234",
		technician.URL,
	)

	test.Eq(t, internal.PostingSource{Platform: "oraclecloud", Key: oracleCloudTestTenant}, technician.Source)

	// A numeric Id must not become "18235.0" or "1.8235e+04" in a URL.
	test.Eq(t, "18235", associate.ExternalID)
	test.Eq(t,
		"https://eluq.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/18235",
		associate.URL,
	)

	// Absent enrichment is absent, never guessed.
	test.Eq(t, "unknown/remote", associate.Location)
	test.Eq(t, internal.EmploymentTypeUnknown, associate.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeUnknown, associate.WorkplaceType)
	test.True(t, associate.PostedAt.IsZero())
}

// TestOracleCloudBuildsTheFinder covers the query parameter that is a small
// language of its own.
//
// "findReqs;siteNumber=CX_1,limit=200,offset=0" carries its structure in a
// semicolon, commas and equals signs, all three of which url.QueryEscape
// escapes — which turns a working request into one Oracle answers with an error
// rather than with jobs.
func TestOracleCloudBuildsTheFinder(t *testing.T) {
	t.Parallel()

	tenant, err := parseOracleCloudTenant("aeo,hcml.fa.us2.oraclecloud.com,AEO-Careers")
	must.NoError(t, err)

	built := oracleCloudListURL(tenant, 400)

	test.StrContains(t, built, "finder=findReqs;siteNumber=AEO-Careers,limit=200,offset=400,sortBy=POSTING_DATES_DESC")
	test.StrContains(t, built, "https://hcml.fa.us2.oraclecloud.com/hcmRestApi/resources/latest/recruitingCEJobRequisitions")
	test.StrContains(t, built, "onlyData=true")

	// It still has to be a URL: a site number carrying a character that would
	// otherwise end the parameter must be escaped, not passed through. An
	// unescaped "&" would silently truncate the finder to "siteNumber=Site Name"
	// and Oracle would answer for a site that does not exist.
	parsed, err := url.Parse(oracleCloudListURL(oracleCloudTenant{host: "h", site: "Site Name&x"}, 0))
	must.NoError(t, err)
	test.StrContains(t, parsed.RawQuery, "siteNumber=Site%20Name%26x,")
	test.Eq(t, "findReqs;siteNumber=Site Name&x,limit=200,offset=0,sortBy=POSTING_DATES_DESC", oracleCloudFinder(parsed.RawQuery))
}

// oracleCloudFinder pulls the finder value out of a request URL's raw query.
//
// Deliberately not through [net/url.Values]: Go's query parser treats a
// semicolon as an error and drops the pair that contains one, and this
// parameter's syntax is built on a semicolon, so Query().Get("finder") is always
// empty here. That is a property of the client library rather than of Oracle,
// which reads the parameter it is sent — but a test that reaches for Query()
// asserts nothing at all, and this file learned that the hard way.
func oracleCloudFinder(rawQuery string) string {
	for _, part := range strings.Split(rawQuery, "&") {
		value, ok := strings.CutPrefix(part, "finder=")
		if !ok {
			continue
		}

		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return value
		}

		return decoded
	}

	return ""
}

func TestOracleCloudFinderEscape(t *testing.T) {
	t.Parallel()

	// The three structural characters survive; everything unsafe does not.
	test.Eq(t, "findReqs;siteNumber=CX_1,limit=200", oracleCloudFinderEscape("findReqs;siteNumber=CX_1,limit=200"))
	test.Eq(t, "AEO-Careers", oracleCloudFinderEscape("AEO-Careers"))
	test.Eq(t, "a%20b", oracleCloudFinderEscape("a b"))
	test.Eq(t, "a%26b", oracleCloudFinderEscape("a&b"))
	test.Eq(t, "a%2Fb%3Fc%23d", oracleCloudFinderEscape("a/b?c#d"))
}

// oracleCloudPage builds a response of exactly count requisitions whose ids
// carry the given prefix, so a full page can be served without the short-page
// check being what ends a pagination loop under test.
func oracleCloudPage(prefix string, count, total int) string {
	requisitions := make([]string, count)

	for i := range requisitions {
		requisitions[i] = fmt.Sprintf(`{"Id":"%s%d","Title":"Job %s%d","PrimaryLocation":"Remote"}`, prefix, i, prefix, i)
	}

	return fmt.Sprintf(`{"items":[{"TotalJobsCount":%d,"requisitionList":[%s]}]}`, total, strings.Join(requisitions, ","))
}

// oracleCloudOffsetTransport serves pages keyed by the offset inside the finder
// parameter, which is the only place this API states one.
type oracleCloudOffsetTransport struct {
	// total is the site's TotalJobsCount, and how many requisitions are served
	// across all pages.
	total int

	// distinct makes every page unique regardless of the total, so only a hard
	// ceiling can end the walk.
	distinct bool

	// perPage caps how many requisitions a page holds regardless of the limit
	// asked for, which is what a server-side page cap looks like. Zero means the
	// requested page size.
	perPage int

	requests int
}

func (o *oracleCloudOffsetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	o.requests++

	offset := 0

	for _, part := range strings.Split(oracleCloudFinder(req.URL.RawQuery), ",") {
		if value, ok := strings.CutPrefix(part, "offset="); ok {
			offset, _ = strconv.Atoi(value)
		}
	}

	count := oracleCloudPageSize
	if o.perPage > 0 {
		count = o.perPage
	}

	if !o.distinct {
		if remaining := o.total - offset; remaining < count {
			count = max(remaining, 0)
		}
	}

	body := oracleCloudPage(strconv.Itoa(offset)+"-", count, o.total)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// TestOracleCloudPaginatesToTheTotal walks a tenant bigger than one page, which
// is the normal case for this platform: the largest registered tenant publishes
// about 16,300 requisitions, or 82 pages.
func TestOracleCloudPaginatesToTheTotal(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: oracleCloudPageSize + 30}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	test.Len(t, oracleCloudPageSize+30, postings)
	test.Eq(t, 2, transport.requests)
}

// TestOracleCloudStopsOnTheReportedTotal covers the cheaper of the two stopping
// conditions: a site whose last page happens to be exactly full is finished, and
// asking for the page after it is a wasted request against a shared host.
func TestOracleCloudStopsOnTheReportedTotal(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: oracleCloudPageSize}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	test.Len(t, oracleCloudPageSize, postings)
	test.Eq(t, 1, transport.requests)
}

// TestOracleCloudKeepsWalkingPastAServerSidePageCap guards against the quietest
// way this adapter could fail.
//
// A short page is the usual signal that a board is finished, but several boards
// in this ecosystem answer with fewer rows than the limit asked for and still
// expect the caller to keep going — ADP's public API is documented to do exactly
// that. Stopping on the first short page would publish 50 of Kroger's ~16,300
// requisitions and report success, which no downstream check could catch.
func TestOracleCloudKeepsWalkingPastAServerSidePageCap(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: 130, perPage: 50}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)

	// 50 + 50 + 30: the offset advances by what each page actually held, so no
	// row is skipped and none is fetched twice.
	test.Len(t, 130, postings)
	test.Eq(t, 3, transport.requests)

	seen := make(map[string]bool, len(postings))
	for _, posting := range postings {
		test.False(t, seen[posting.URL], test.Sprintf("posting %q was yielded twice", posting.URL))
		seen[posting.URL] = true
	}
}

// TestOracleCloudStopsWhenTheSiteIgnoresOffset is a regression test for the
// failure this package has just finished repairing in eight adapters: a tenant
// that answers every offset with the same first page never sends a short one, so
// a loop that ends only on a short page runs until the crawl deadline, pinning a
// worker and hammering one host, while internal.Dedupe hides the duplicates.
func TestOracleCloudStopsWhenTheSiteIgnoresOffset(t *testing.T) {
	t.Parallel()

	// A total large enough that the count can never end the walk either.
	client, transport := repeatingPageClient(oracleCloudPage("", oracleCloudPageSize, 1_000_000))

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the walk before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, oracleCloudPageSize, postings)
}

// TestOracleCloudStopsAtItsPageCeiling covers the backstop for the case a
// repeated page cannot catch: a site that keeps serving different full pages
// forever. Hitting the ceiling is reported rather than passed off as the end of
// a board.
func TestOracleCloudStopsAtItsPageCeiling(t *testing.T) {
	t.Parallel()

	transport := &oracleCloudOffsetTransport{total: 1_000_000, distinct: true}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	test.Eq(t, oracleCloudMaxPages, transport.requests)
	test.Len(t, oracleCloudMaxPages*oracleCloudPageSize, postings)

	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "refusing to keep paginating")
}

// TestOracleCloudStopsWhenTheConsumerDoes guards the iterator contract the
// health command depends on: it caps each source at 100 postings by returning
// false from yield, and an adapter that keeps fetching afterwards both burns the
// budget the cap exists to save and risks calling yield again, which panics.
func TestOracleCloudStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(oracleCloudPage("", oracleCloudPageSize, 1_000_000))

	var seen int

	for range OracleCloud(t.Context(), client, oracleCloudTestTenant) {
		seen++

		if seen == 5 {
			break
		}
	}

	test.Eq(t, 5, seen)
	test.Eq(t, 1, transport.requests)
}

// TestOracleCloudReportsAnUnreadableResponse covers the shapes that must never
// be mistaken for an employer with no openings. A silently-empty source is the
// worst failure this project has, and every case here answers HTTP 200.
func TestOracleCloudReportsAnUnreadableResponse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want string
	}{
		// The envelope is always one item, even for a site with no jobs, so an
		// empty array is Oracle answering something other than this API.
		"no items": {
			body: `{"items":[]}`,
			want: "no items in the requisition list response",
		},
		// Neither a list nor a count means the shape is not the one this
		// adapter was written against.
		"neither a list nor a count": {
			body: `{"items":[{}]}`,
			want: "layout may have changed",
		},
		"a renamed requisition list": {
			body: `{"items":[{"requisitions":[{"Id":"1"}]}]}`,
			want: "layout may have changed",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, _ := fixtureClient(map[string]string{"eluq.fa.us2.oraclecloud.com": tc.body})

			postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

			test.SliceEmpty(t, postings)
			must.Len(t, 1, errs)
			must.StrContains(t, errs[0].Error(), tc.want)

			// Among ~1,800 sources an error that does not name its tenant is
			// unattributable.
			must.StrContains(t, errs[0].Error(), "acme")
			must.StrContains(t, errs[0].Error(), "CX_1")
		})
	}
}

// TestOracleCloudAcceptsAnEmptySite is the other half of that rule: a site that
// answers with this API's envelope and reports zero jobs is a careers site with
// nothing open, which is not an error.
func TestOracleCloudAcceptsAnEmptySite(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"eluq.fa.us2.oraclecloud.com": `{"items":[{"TotalJobsCount":0,"requisitionList":[]}]}`,
	})

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

func TestOracleCloudReportsANon200(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"eluq.fa.us2.oraclecloud.com": `{}`},
		status: http.StatusInternalServerError,
	}

	postings, errs := drain(OracleCloud(t.Context(), &http.Client{Transport: transport}, oracleCloudTestTenant))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestOracleCloudSurvivesRetypedFields is the Jibe "meta_data" lesson applied
// before it costs anything: fetchJSON decodes a whole page at once, so one field
// arriving with an unexpected JSON type takes down every posting on it. The
// fields whose type nobody here has confirmed against a real response are
// therefore `any`, and this is what proves it.
func TestOracleCloudSurvivesRetypedFields(t *testing.T) {
	t.Parallel()

	const retyped = `{"items":[{"TotalJobsCount":"1","requisitionList":[
		{"Id":9001,"Title":"Analyst","PrimaryLocation":"Remote","PostedDate":null,
		 "JobType":["Full time"],"WorkplaceTypeCode":{"code":"ORA_REMOTE"},"JobFunction":false}
	]}]}`

	client, transport := fixtureClient(map[string]string{"eluq.fa.us2.oraclecloud.com": retyped})

	postings, errs := drain(OracleCloud(t.Context(), client, oracleCloudTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	// A count that arrives as a quoted number is still a count, so the walk ends
	// on it rather than asking for a page that does not exist.
	must.Len(t, 1, transport.requests)

	test.Eq(t, "9001", postings[0].ExternalID)
	test.Eq(t, "Analyst", postings[0].Title)

	// A single-element array is read as the value it wraps, which is what
	// anyText already does for BambooHR.
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)

	// An object is not a scalar, so it renders as nothing rather than as Go's
	// spelling of a map. Absent beats wrong: "map[code:ORA_REMOTE]" would not
	// normalize to a workplace type anyway, but it would render into a
	// department or a location on the next field that is typed this way.
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[0].WorkplaceType)
	test.Eq(t, "", postings[0].Department)

	// A null date is no date, not the epoch.
	test.True(t, postings[0].PostedAt.IsZero())
}

func TestOracleCloudRejectsAMalformedTenant(t *testing.T) {
	t.Parallel()

	badKeys := []string{
		"acme",
		"acme,eluq.fa.us2.oraclecloud.com",
		"acme,eluq.fa.us2.oraclecloud.com,CX_1,extra",
		"acme,,CX_1",
		"acme,eluq.fa.us2.oraclecloud.com,",
	}

	for _, key := range badKeys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			client, transport := fixtureClient(map[string]string{"oraclecloud.com": oracleCloudListFixture})

			postings, errs := drain(OracleCloud(t.Context(), client, key))

			test.SliceEmpty(t, postings)
			test.SliceEmpty(t, transport.requests)
			must.Len(t, 1, errs)
			must.StrContains(t, errs[0].Error(), "invalid Oracle Cloud tenant")

			test.Eq(t, key, oracleCloudCompanyName(key))
		})
	}
}

// TestOracleCloudAddsNoDoubleCountedEmployer is the twin of the SuccessFactors
// check, and the one that found a real overlap: Marriott is the second-largest
// tenant in the Oracle candidate file and was already registered on Jibe, so
// adding it would have counted ~11,900 postings twice in a trend line that
// cannot tell that apart from hiring.
func TestOracleCloudAddsNoDoubleCountedEmployer(t *testing.T) {
	t.Parallel()

	elsewhere := companiesOnOtherPlatforms(oracleCloudPlatform)

	for _, key := range OracleCloudTenants {
		company := oracleCloudCompanyName(key)

		platform, clash := elsewhere[strings.ToLower(company)]

		test.False(t, clash, test.Sprintf("company %q is registered on both oraclecloud and %s, so its postings would be counted twice; pick one route", company, platform))
	}
}

// TestOracleCloudTenantsComeFromTheCandidateFile keeps the registered list
// honest about its own provenance, for the reasons spelled out on its
// SuccessFactors twin: the registered set is a hand-picked staging subset of a
// much larger unprobed candidate file, and a triple that is not in that file was
// either typed from memory or edited after the fact.
func TestOracleCloudTenantsComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateTenants(t, "oracle_orc_tenants.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(OracleCloudTenants))

	for _, key := range OracleCloudTenants {
		tenant, err := parseOracleCloudTenant(key)
		must.NoError(t, err, must.Sprintf("registered tenant %q", key))

		test.False(t, seen[tenant.slug], test.Sprintf("company %q is registered twice", tenant.slug))
		seen[tenant.slug] = true

		test.True(t, candidates[key], test.Sprintf("registered tenant %q is not in testdata/candidates/oracle_orc_tenants.txt", key))

		// Every registered host is under one suffix, which is what lets a single
		// servicePolicyFor entry keep the whole platform inside one politeness
		// budget. A tenant on some other host would silently escape it.
		test.StrHasSuffix(t, ".oraclecloud.com", tenant.host)
	}

	test.Less(t, len(candidates), len(OracleCloudTenants), test.Sprint("the registered list should stay a subset of the candidates"))
}

// TestOracleCloudFixtureMatchesTheDecodedShape keeps the fixture honest: it is
// the only description of this API in the repository, so a typo in it would be
// invisible and would make every other test in this file pass against a shape
// the real service never sends.
func TestOracleCloudFixtureMatchesTheDecodedShape(t *testing.T) {
	t.Parallel()

	var envelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}

	must.NoError(t, json.Unmarshal([]byte(oracleCloudListFixture), &envelope))
	must.Len(t, 1, envelope.Items)

	for _, key := range []string{"TotalJobsCount", "requisitionList"} {
		_, ok := envelope.Items[0][key]
		test.True(t, ok, test.Sprintf("fixture item is missing %q", key))
	}
}
