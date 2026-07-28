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

func TestEightfold(t *testing.T) {
	testSingle(t, "bayer", Eightfold)
}

func TestEightfold_all(t *testing.T) {
	testMultipleParallel(t, slices.Values(EightfoldCompanies), Eightfold)
}

// eightfoldPageTransport serves one body per exact request URL.
//
// The shared fixtureTransport matches on a URL substring, and every page of an
// Eightfold board shares the substring "/api/apply/v2/jobs", so a paginated
// fixture built on it would serve whichever route the map happened to iterate to
// first. Matching the whole URL keeps these tests deterministic.
type eightfoldPageTransport struct {
	pages    map[string]string
	requests []string
}

func (tr *eightfoldPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.requests = append(tr.requests, req.URL.String())

	body, ok := tr.pages[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     http.StatusText(http.StatusNotFound),
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"message":"no fixture"}`)),
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// eightfoldPageURL builds the URL the adapter requests for one page, so the
// fixtures below cannot drift from the adapter's own URL construction.
func eightfoldPageURL(company string, start int) string {
	return fmt.Sprintf("https://%s.eightfold.ai/api/apply/v2/jobs?start=%d&num=%d",
		company, start, eightfoldPageSize)
}

// eightfoldPositions renders count filler positions starting at the given id, so
// a test that only cares about paging does not carry a page of hand-written
// JSON.
func eightfoldPositions(firstID, count int) string {
	items := make([]string, 0, count)

	for i := range count {
		items = append(items, fmt.Sprintf(
			`{"id":%d,"name":"Engineer %d","location":"Berlin,Germany","canonicalPositionUrl":"https://talent.example.com/careers/job/%d"}`,
			firstID+i, firstID+i, firstID+i))
	}

	return strings.Join(items, ",")
}

func TestEightfoldParsesPostings(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"bayer.eightfold.ai": `{
			"count": 2,
			"positions": [
				{
					"id": 562949978274760,
					"name": "  Senior Medical Science Liaison  ",
					"location": "  Residence Based,Rhode Island,United States  ",
					"department": "Medical Affairs & Pharmacovigilance",
					"business_unit": "Pharmaceuticals",
					"t_create": 1784678400,
					"t_update": 1784942071,
					"display_job_id": "877989",
					"work_location_option": "onsite",
					"canonicalPositionUrl": "https://talent.bayer.com/careers/job/562949978274760"
				},
				{
					"id": 562949978276668,
					"name": "Staff Engineer",
					"location": "",
					"work_location_option": "remote_local",
					"canonicalPositionUrl": "https://talent.bayer.com/careers/job/562949978276668"
				}
			]
		}`,
	})

	postings, errs := drain(Eightfold(t.Context(), client, "bayer"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	first := postings[0]
	test.Eq(t, "bayer", first.Company)
	test.Eq(t, "https://talent.bayer.com/careers/job/562949978274760", first.URL)
	test.Eq(t, "Senior Medical Science Liaison", first.Title)
	test.Eq(t, "Residence Based,Rhode Island,United States", first.Location)
	test.Eq(t, "Medical Affairs & Pharmacovigilance", first.Department)
	test.Eq(t, "Pharmaceuticals", first.Team)
	test.Eq(t, "877989", first.RequisitionID)
	test.Eq(t, "562949978274760", first.ExternalID)
	test.Eq(t, internal.WorkplaceTypeOnsite, first.WorkplaceType)
	test.Eq(t, time.Unix(1784678400, 0).UTC(), first.PostedAt)
	test.Eq(t, time.Unix(1784942071, 0).UTC(), first.UpdatedAt)
	test.Eq(t, internal.PostingSource{Platform: "eightfold", Key: "bayer"}, first.Source)

	// An onsite posting must leave Remote unset rather than false, so
	// [internal.JobPosting.IsRemote]'s text fallback still runs; see
	// TestEightfoldLeavesRemoteUnsetUnlessTheBoardSaysRemote.
	test.Nil(t, first.Remote)

	// A board that publishes no location must still produce a usable posting,
	// and an absent date must stay the zero time rather than becoming 1970.
	second := postings[1]
	test.Eq(t, "unknown/remote", second.Location)
	test.Eq(t, internal.WorkplaceTypeRemote, second.WorkplaceType)
	test.True(t, second.PostedAt.IsZero())
	test.True(t, second.UpdatedAt.IsZero())
	test.Eq(t, "", second.Department)

	must.NotNil(t, second.Remote)
	test.True(t, *second.Remote)
	test.True(t, second.IsRemote())
}

// TestEightfoldLeavesRemoteUnsetUnlessTheBoardSaysRemote pins the asymmetry in
// [eightfoldRemote], which is the whole point of that function.
//
// `--remote` filters on [internal.JobPosting.IsRemote], which reads Remote when
// it is set and otherwise searches the location and title text. Setting Remote
// true where Eightfold says remote wins postings the text search cannot see.
// Setting it *false* for onsite and hybrid would be symmetric and would lose
// postings: measured on the registered tenants, a Netflix posting located
// "USA - Remote" is flagged onsite and a Liberty Mutual one located
// "Remote, Remote, United States" is flagged hybrid. Both must stay findable.
func TestEightfoldLeavesRemoteUnsetUnlessTheBoardSaysRemote(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"netflix.eightfold.ai": `{
			"positions": [
				{"id": 1, "name": "AI Engineer 6", "location": "USA - Remote", "work_location_option": "onsite"},
				{"id": 2, "name": "Claims Adjuster", "location": "Remote, Remote, United States", "work_location_option": "hybrid"},
				{"id": 3, "name": "Staff Engineer", "location": "New York,New York,United States", "work_location_option": "remote_local"},
				{"id": 4, "name": "Technician", "location": "Los Gatos, California", "work_location_option": "onsite"}
			]
		}`,
	})

	postings, errs := drain(Eightfold(t.Context(), client, "netflix"))

	must.SliceEmpty(t, errs)
	must.Len(t, 4, postings)

	// Flagged onsite/hybrid but the location says remote: Remote stays unset so
	// the text heuristic still finds them.
	test.Nil(t, postings[0].Remote)
	test.True(t, postings[0].IsRemote())
	test.Nil(t, postings[1].Remote)
	test.True(t, postings[1].IsRemote())

	// Flagged remote but located in a named city: only the structured flag can
	// find this one, which is the case the flag is set for.
	must.NotNil(t, postings[2].Remote)
	test.True(t, postings[2].IsRemote())

	// Genuinely onsite stays not-remote, via the heuristic rather than a false.
	test.Nil(t, postings[3].Remote)
	test.False(t, postings[3].IsRemote())
}

// TestEightfoldReportsHittingThePageCeiling checks that exhausting the bound is
// an error rather than a quiet truncation.
//
// A board still serving full pages at the ceiling has been cut off mid-list.
// Returning nil there would make `health` call the source ok and hide the exact
// failure the ceiling exists to catch.
func TestEightfoldReportsHittingThePageCeiling(t *testing.T) {
	t.Parallel()

	// Every offset answers with a distinct full page, so neither the short-page
	// stop nor pageRepeatGuard can end the walk.
	transport := &eightfoldEndlessTransport{}

	postings, errs := drain(Eightfold(t.Context(), &http.Client{Transport: transport}, "endless"))

	test.Len(t, eightfoldMaxPages*eightfoldPageSize, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"endless"`)
	test.StrContains(t, errs[0].Error(), "refusing to keep paginating Eightfold")
}

// eightfoldEndlessTransport answers every offset with a distinct full page, the
// one shape that reaches the page ceiling.
type eightfoldEndlessTransport struct {
	page int
}

func (tr *eightfoldEndlessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.page++

	body := `{"positions":[` + eightfoldPositions(tr.page*eightfoldPageSize, eightfoldPageSize) + `]}`

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// TestEightfoldToleratesPolymorphicDepartment is a regression test for a real
// tenant, not a hypothetical.
//
// Fluor — 716 postings, the second-largest tenant registered here — sends
// department as a JSON array, while every other tenant sends a bare string and
// some send null. fetchJSON decodes a whole page at once, so modelling the field
// as a Go string would have failed the decode for the page and cost Fluor every
// one of its postings, silently.
func TestEightfoldToleratesPolymorphicDepartment(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"fluor.eightfold.ai": `{
			"count": 5,
			"positions": [
				{"id": 1, "name": "Pipefitter Welder", "location": "Martin, US-SC", "department": ["Pipefitter"]},
				{"id": 2, "name": "Custodian Lead", "location": "Warrenton, US-VA", "department": "Operations & Maintenance"},
				{"id": 3, "name": "Planner", "location": "Greenville, US-SC", "department": null},
				{"id": 4, "name": "Estimator", "location": "Houston, US-TX", "department": {"name": "Estimating"}},
				{"id": 5, "name": "Scheduler", "location": "Aiken, US-SC", "department": ["", "Project Controls"]}
			]
		}`,
	})

	postings, errs := drain(Eightfold(t.Context(), client, "fluor"))

	must.SliceEmpty(t, errs)
	must.Len(t, 5, postings)

	test.Eq(t, "Pipefitter", postings[0].Department)
	test.Eq(t, "Operations & Maintenance", postings[1].Department)
	test.Eq(t, "", postings[2].Department)

	// An object is not a label; publishing its literal JSON would put "{...}"
	// into an employer's department.
	test.Eq(t, "", postings[3].Department)

	// The first *non-empty* entry, so a leading blank does not blank the field.
	test.Eq(t, "Project Controls", postings[4].Department)
}

func TestEightfoldFallsBackToTheTenantURL(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"vale.eightfold.ai": `{"count":1,"positions":[{"id":43157555,"name":"Geologist","location":"Brazil"}]}`,
	})

	postings, errs := drain(Eightfold(t.Context(), client, "vale"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	test.Eq(t, "https://vale.eightfold.ai/careers/job/43157555", postings[0].URL)
}

func TestEightfoldPagesUntilAShortPage(t *testing.T) {
	t.Parallel()

	transport := &eightfoldPageTransport{pages: map[string]string{
		eightfoldPageURL("hsbc", 0):  `{"count":25,"positions":[` + eightfoldPositions(1, 10) + `]}`,
		eightfoldPageURL("hsbc", 10): `{"count":25,"positions":[` + eightfoldPositions(11, 10) + `]}`,
		eightfoldPageURL("hsbc", 20): `{"count":25,"positions":[` + eightfoldPositions(21, 5) + `]}`,
	}}

	postings, errs := drain(Eightfold(t.Context(), &http.Client{Transport: transport}, "hsbc"))

	must.SliceEmpty(t, errs)
	test.Len(t, 25, postings)
	test.Len(t, 3, transport.requests)
}

// TestEightfoldEndsOnAnEmptyPage covers the board whose posting count is an
// exact multiple of the page size, so no page is ever short. It costs one extra
// request, which is the price of not trusting "count".
func TestEightfoldEndsOnAnEmptyPage(t *testing.T) {
	t.Parallel()

	transport := &eightfoldPageTransport{pages: map[string]string{
		eightfoldPageURL("netapp", 0):  `{"count":20,"positions":[` + eightfoldPositions(1, 10) + `]}`,
		eightfoldPageURL("netapp", 10): `{"count":20,"positions":[` + eightfoldPositions(11, 10) + `]}`,
		eightfoldPageURL("netapp", 20): `{"count":20,"positions":[]}`,
	}}

	postings, errs := drain(Eightfold(t.Context(), &http.Client{Transport: transport}, "netapp"))

	must.SliceEmpty(t, errs)
	test.Len(t, 20, postings)
	test.Len(t, 3, transport.requests)
}

// TestEightfoldTrustsThePagesOverTheCount guards the direction this adapter
// deliberately fails in. A "count" lower than what the board actually serves
// must not truncate the board; only a short page ends the walk.
//
// This is not hypothetical tidiness: the first draft of the adapter did stop on
// "count", and this test is what caught it.
func TestEightfoldTrustsThePagesOverTheCount(t *testing.T) {
	t.Parallel()

	transport := &eightfoldPageTransport{pages: map[string]string{
		eightfoldPageURL("costar", 0):  `{"count":3,"positions":[` + eightfoldPositions(1, 10) + `]}`,
		eightfoldPageURL("costar", 10): `{"count":3,"positions":[` + eightfoldPositions(11, 2) + `]}`,
	}}

	postings, errs := drain(Eightfold(t.Context(), &http.Client{Transport: transport}, "costar"))

	must.SliceEmpty(t, errs)
	test.Len(t, 12, postings)
}

func TestEightfoldTreatsAnEmptyBoardAsNoError(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"aexp.eightfold.ai": `{"count":0,"positions":[]}`,
	})

	postings, errs := drain(Eightfold(t.Context(), client, "aexp"))

	test.SliceEmpty(t, errs)
	test.SliceEmpty(t, postings)
}

// TestEightfoldStopsWhenTheBoardIgnoresStart covers the failure mode
// [pageRepeatGuard] exists for: a tenant that answers every offset with the same
// full first page would otherwise page until the crawl deadline.
func TestEightfoldStopsWhenTheBoardIgnoresStart(t *testing.T) {
	t.Parallel()

	page := `{"count":9999,"positions":[` + eightfoldPositions(1, 10) + `]}`

	transport := &eightfoldPageTransport{pages: map[string]string{
		eightfoldPageURL("stuck", 0):  page,
		eightfoldPageURL("stuck", 10): page,
		eightfoldPageURL("stuck", 20): page,
	}}

	postings, errs := drain(Eightfold(t.Context(), &http.Client{Transport: transport}, "stuck"))

	must.SliceEmpty(t, errs)
	test.Len(t, 10, postings)
	test.Len(t, 2, transport.requests)
}

// TestEightfoldReportsTheAuthorizationWall pins the signal an operator sees for
// the roughly three quarters of Eightfold tenants that gate this endpoint. It
// has to name the company and carry the status, or a failure among ~2,200
// sources is unattributable and a walled tenant looks like a dead one.
func TestEightfoldReportsTheAuthorizationWall(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"qualcomm.eightfold.ai": `{"message": "Not authorized for PCSX"}`,
	})
	transport.status = http.StatusForbidden

	postings, errs := drain(Eightfold(t.Context(), client, "qualcomm"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"qualcomm"`)
	test.StrContains(t, errs[0].Error(), "Forbidden")
}

func TestEightfoldReportsHTTPError(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"nutanix.eightfold.ai": `{}`,
	})
	transport.status = http.StatusNotFound

	postings, errs := drain(Eightfold(t.Context(), client, "nutanix"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"nutanix"`)
}

func TestEightfoldReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"bayer.eightfold.ai": `{"positions": [`,
	})

	postings, errs := drain(Eightfold(t.Context(), client, "bayer"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), `"bayer"`)
}

func TestEightfoldStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	transport := &eightfoldPageTransport{pages: map[string]string{
		eightfoldPageURL("hsbc", 0):  `{"count":30,"positions":[` + eightfoldPositions(1, 10) + `]}`,
		eightfoldPageURL("hsbc", 10): `{"count":30,"positions":[` + eightfoldPositions(11, 10) + `]}`,
	}}

	var seen int

	for range Eightfold(t.Context(), &http.Client{Transport: transport}, "hsbc") {
		seen++

		break
	}

	test.Eq(t, 1, seen)
	test.Len(t, 1, transport.requests)
}

func TestEightfoldTimestamp(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		seconds int64
		want    time.Time
	}{
		{"seconds", 1784678400, time.Unix(1784678400, 0).UTC()},
		{"absent", 0, time.Time{}},
		{"negative", -1, time.Time{}},
		// A tenant that switched to milliseconds would otherwise date every
		// posting to the year 58000 and satisfy every --posted-since query.
		{"milliseconds are rejected", 1784678400000, time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tc.want, eightfoldTimestamp(tc.seconds))
		})
	}
}

func TestEightfoldWorkplaceType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want internal.WorkplaceType
	}{
		{"onsite", internal.WorkplaceTypeOnsite},
		{"hybrid", internal.WorkplaceTypeHybrid},
		{"remote_local", internal.WorkplaceTypeRemote},
		{"remote_global", internal.WorkplaceTypeRemote},
		{"", internal.WorkplaceTypeUnknown},
		{"something_new", internal.WorkplaceTypeUnknown},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tc.want, eightfoldWorkplaceType(tc.raw))
		})
	}
}

// TestEightfoldCompaniesComeFromTheCandidateFile keeps the registered list
// honest: every slug in it is one the discovery pass actually recorded, and the
// registered set stays the verified subset rather than the whole harvest.
func TestEightfoldCompaniesComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "eightfold_slugs.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(EightfoldCompanies))

	for _, slug := range EightfoldCompanies {
		test.False(t, seen[slug], test.Sprintf("company %q is registered twice", slug))
		seen[slug] = true

		test.True(t, candidates[slug], test.Sprintf("registered company %q is not in testdata/candidates/eightfold_slugs.txt", slug))
	}

	test.Less(t, len(candidates), len(EightfoldCompanies), test.Sprint("the registered list should stay a subset of the candidates"))
}

func TestEightfoldCompaniesAreSortedAndDeduplicated(t *testing.T) {
	t.Parallel()

	test.True(t, slices.IsSorted(EightfoldCompanies), test.Sprint("tenant list is not sorted"))

	seen := make(map[string]struct{}, len(EightfoldCompanies))

	for _, slug := range EightfoldCompanies {
		_, duplicate := seen[slug]
		test.False(t, duplicate, test.Sprintf("duplicate tenant %q", slug))
		seen[slug] = struct{}{}
	}
}
