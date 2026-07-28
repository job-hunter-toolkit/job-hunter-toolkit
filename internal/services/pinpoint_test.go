package services

import (
	"net/http"
	"slices"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPinpoint(t *testing.T) {
	testSingle(t, "surrealdb", Pinpoint)
}

func TestPinpoint_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	testMultipleParallel(t, slices.Values(PinpointCompanies), Pinpoint)
}

// TestPinpointParsesPostings covers the three-state workplace type — the whole
// reason internal.WorkplaceType exists, since Remote *bool cannot say "hybrid" —
// along with the department, employment type and employer-published pay this
// platform carries.
func TestPinpointParsesPostings(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.pinpointhq.com/postings.json": `{
			"data": [
				{
					"id": 883421,
					"title": "  Security Engineer  ",
					"url": "https://acme.pinpointhq.com/postings/883421",
					"description": "<p>ignored</p>",
					"employment_type_text": "Full-Time",
					"workplace_type": "remote",
					"compensation_visible": true,
					"compensation_minimum": 120000,
					"compensation_maximum": "150000",
					"compensation_currency": "usd",
					"location": {"city": "", "name": "Remote - US", "province": ""},
					"job": {"department": {"name": "Engineering"}}
				},
				{
					"id": "883422",
					"title": "Support Specialist",
					"url": "https://acme.pinpointhq.com/postings/883422",
					"employment_type_text": "Part-Time",
					"workplace_type": "hybrid",
					"compensation_visible": false,
					"compensation_minimum": 45000,
					"compensation_maximum": 55000,
					"compensation_currency": "GBP",
					"location": {"city": "Bristol", "province": "England", "name": "Bristol HQ"},
					"job": {"department": {"name": "Customer Success"}}
				},
				{
					"id": 883423,
					"title": "Warehouse Associate",
					"url": "https://acme.pinpointhq.com/postings/883423",
					"employment_type_text": "Seasonal",
					"workplace_type": "on_site",
					"location": {"name": "Leeds"}
				},
				{
					"id": 883424,
					"title": "No link",
					"workplace_type": "remote"
				}
			]
		}`,
	})

	postings, errs := drain(Pinpoint(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	engineer := postings[0]

	test.Eq(t, "Security Engineer", engineer.Title)
	test.Eq(t, "acme", engineer.Company)
	test.Eq(t, "https://acme.pinpointhq.com/postings/883421", engineer.URL)
	test.Eq(t, "Remote - US", engineer.Location)
	test.Eq(t, "Engineering", engineer.Department)
	test.Eq(t, internal.EmploymentTypeFullTime, engineer.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, engineer.WorkplaceType)
	test.Eq(t, "883421", engineer.ExternalID)
	test.Eq(t, internal.PostingSource{Platform: "pinpoint", Key: "acme"}, engineer.Source)

	must.NotNil(t, engineer.Remote)
	test.True(t, *engineer.Remote)

	must.NotNil(t, engineer.Compensation)
	test.Eq(t, 120000.0, engineer.Compensation.Min)
	test.Eq(t, 150000.0, engineer.Compensation.Max)
	test.Eq(t, "USD", engineer.Compensation.Currency)
	test.Eq(t, internal.ProvenanceEmployer, engineer.Compensation.Provenance)

	// Pinpoint publishes no posted date, so a --posted-since query excludes these
	// rather than being filled with synthesized ones.
	test.True(t, engineer.PostedAt.IsZero())

	support := postings[1]

	test.Eq(t, "Bristol, England", support.Location)
	test.Eq(t, internal.WorkplaceTypeHybrid, support.WorkplaceType)
	test.Eq(t, "883422", support.ExternalID)

	// The employer chose one of three published values, so "not fully remote" is
	// a statement rather than an absence.
	must.NotNil(t, support.Remote)
	test.False(t, *support.Remote)

	// compensation_visible is the employer's own switch, and the numbers are in
	// the response either way. Reading them anyway would publish pay an employer
	// deliberately hid.
	test.Nil(t, support.Compensation)

	warehouse := postings[2]

	test.Eq(t, "Leeds", warehouse.Location)
	test.Eq(t, internal.WorkplaceTypeOnsite, warehouse.WorkplaceType)
	test.Eq(t, internal.EmploymentTypeTemporary, warehouse.EmploymentType)

	must.NotNil(t, warehouse.Remote)
	test.False(t, *warehouse.Remote)
}

func TestPinpointReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"pinpointhq.com": `{}`},
		status: http.StatusForbidden,
	}

	postings, errs := drain(Pinpoint(t.Context(), &http.Client{Transport: transport}, "gone"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "gone")
}

func TestPinpointReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"pinpointhq.com": `{"data": [`})

	postings, errs := drain(Pinpoint(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestPinpointReportsAResponseWithNoData covers the silently-empty failure this
// project treats as its worst: a 200 that is not the postings feed decodes into
// an empty list, and reporting that as "this company is not hiring" would hide
// the break.
func TestPinpointReportsAResponseWithNoData(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"pinpointhq.com": `{"postings": []}`})

	postings, errs := drain(Pinpoint(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "data")
}

// TestPinpointReportsEmptyBoardWithoutError is the other half of that
// distinction: an empty data array is a real answer from a company that is not
// hiring today.
func TestPinpointReportsEmptyBoardWithoutError(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"pinpointhq.com": `{"data": []}`})

	postings, errs := drain(Pinpoint(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

// TestPinpointReportsAJSONAPIEnvelope is the named risk of this adapter. "data"
// is also JSON:API's container, and under that convention a posting's fields
// live in a nested "attributes" object rather than on the element itself. Every
// posting then decodes into an empty struct, which is exactly a full board
// reporting zero — so it is reported as the shape change it is. This fixture is
// the shape a live verification pass should check for before promoting more
// tenants.
func TestPinpointReportsAJSONAPIEnvelope(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"pinpointhq.com": `{"data": [
			{"id": "883421", "type": "postings", "attributes": {
				"title": "Security Engineer",
				"url": "https://acme.pinpointhq.com/postings/883421"
			}},
			{"id": "883422", "type": "postings", "attributes": {
				"title": "Support Specialist",
				"url": "https://acme.pinpointhq.com/postings/883422"
			}}
		]}`,
	})

	postings, errs := drain(Pinpoint(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "2 postings decoded")
}

// TestPinpointCompaniesComeFromTheCandidateFile keeps the registered list
// honest: every slug in it is one a research pass actually recorded, and the
// registered set stays a small staged subset rather than the whole unprobed
// harvest.
func TestPinpointCompaniesComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "pinpoint_tenants.txt")

	must.Greater(t, 50, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	// surrealdb is the one registered tenant with a different provenance: it
	// comes from this repository's own docs/source-backlog.md, which recorded
	// https://surrealdb.pinpointhq.com from a live fingerprinting pass, and
	// registering this platform is what closes that backlog row. It is listed
	// here rather than added to the candidate file so the file stays a verbatim
	// copy of what the research pass produced.
	fromBacklog := map[string]bool{"surrealdb": true}

	seen := make(map[string]bool, len(PinpointCompanies))

	for _, slug := range PinpointCompanies {
		test.False(t, seen[slug], test.Sprintf("company %q is registered twice", slug))
		seen[slug] = true

		if fromBacklog[slug] {
			continue
		}

		test.True(t, candidates[slug], test.Sprintf("registered company %q is not in testdata/candidates/pinpoint_tenants.txt", slug))
	}

	test.Less(t, len(candidates), len(PinpointCompanies), test.Sprint("the registered list should stay a subset of the candidates"))
}
