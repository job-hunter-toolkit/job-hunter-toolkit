package services

import (
	"net/http"
	"os"
	"path/filepath"
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

// pinpointFixture reads a response captured from a live Pinpoint board.
//
// The capture under testdata is what the board answered with the four HTML
// blocks removed — description, benefits, key_responsibilities and
// skills_knowledge_expertise, together about 95% of the bytes, none of which
// this adapter decodes. Every other key, and every value, is the board's own.
func pinpointFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// TestPinpointParsesACapturedLiveBoard is the fixture that decides whether this
// adapter reads Pinpoint, as opposed to reading the shape a document said
// Pinpoint has. The body is https://jed.pinpointhq.com/postings.json as
// captured on 2026-07-28.
//
// What the capture establishes, and what the hand-written fixture above cannot:
//
//   - compensation_frequency exists. docs/research/ats-platform-survey.md does
//     not list it and this adapter's own comment used to assert Pinpoint
//     publishes no period at all. It is present on all 6,406 postings measured
//     across the platform and populated on 3,002 of the 3,169 that show pay.
//   - it is load-bearing. The hourly posting here pays 31.25 and the annual ones
//     pay 105,000 — but a monthly range of 4,500 is indistinguishable from an
//     annual one by magnitude, and 113 live postings publish exactly that. The
//     inference [internal.Compensation] falls back to only ever answers hour or
//     year.
//   - the money arrives as JSON floats ("105000.0"), and the ids as strings.
//     pinpointScalar has to absorb both, and a float64 field would have been
//     fine here and fatal on the first tenant that quoted a figure.
//   - workplace_type's third state is spelled "onsite". The survey says
//     "on_site", and no posting on the platform sends that.
//   - location.city can be a real city while location.name says "Remote", so the
//     city/province preference in [pinpointLocation] is what keeps a New York
//     posting from being filed under "Remote".
func TestPinpointParsesACapturedLiveBoard(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"jed.pinpointhq.com/postings.json": pinpointFixture(t, "pinpoint_jed_postings.json"),
	})

	postings, errs := drain(Pinpoint(t.Context(), client, "jed"))

	must.SliceEmpty(t, errs)
	must.Len(t, 4, postings)

	first := postings[0]

	test.Eq(t, "jed", first.Company)
	test.Eq(t, "Senior Data Management Specialist", first.Title)
	test.Eq(t, "https://jed.pinpointhq.com/en/postings/fceb8b47-bed5-45bd-b7f4-9161fbe78428", first.URL)
	test.Eq(t, "New York City, New York", first.Location)
	test.Eq(t, "KNA-Research & Evaluation", first.Department)
	test.Eq(t, "541307", first.ExternalID)
	test.Eq(t, internal.EmploymentTypeFullTime, first.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, first.WorkplaceType)
	test.Eq(t, internal.PostingSource{Platform: "pinpoint", Key: "jed"}, first.Source)

	must.NotNil(t, first.Remote)
	test.True(t, *first.Remote)

	// No posting on this platform carries a posted date; deadline_at is the only
	// date in the response and it is not one.
	test.True(t, first.PostedAt.IsZero())

	must.NotNil(t, first.Compensation)
	test.Eq(t, 105000.0, first.Compensation.Min)
	test.Eq(t, 135000.0, first.Compensation.Max)
	test.Eq(t, "USD", first.Compensation.Currency)
	test.Eq(t, internal.PeriodYear, first.Compensation.Period)
	test.Eq(t, internal.ProvenanceEmployer, first.Compensation.Provenance)

	// The fellowship is the reason compensation_frequency is read: it is an
	// hourly rate on a board whose other rows are annual salaries, and the board
	// says so rather than leaving it to be guessed.
	fellowship := postings[2]

	test.Eq(t, internal.WorkplaceTypeHybrid, fellowship.WorkplaceType)
	test.Eq(t, "ALL, Texas", fellowship.Location)

	must.NotNil(t, fellowship.Compensation)
	test.Eq(t, 31.25, fellowship.Compensation.Min)
	test.Eq(t, internal.PeriodHour, fellowship.Compensation.Period)

	must.NotNil(t, fellowship.Remote)
	test.False(t, *fellowship.Remote)
}

// TestPinpointIgnoresAnUnmappablePayPeriod pins the one live
// compensation_frequency value [internal.Period] has no unit for. Folding
// "two_weeks" into "week" would halve every figure it touched while looking
// exactly like a correct answer, so it falls back to the magnitude heuristic
// instead, which is where it was before the field was read at all.
func TestPinpointIgnoresAnUnmappablePayPeriod(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"pinpointhq.com": `{"data": [{
			"id": "1",
			"title": "Line Cook",
			"url": "https://acme.pinpointhq.com/en/postings/1",
			"compensation_visible": true,
			"compensation_minimum": 1800,
			"compensation_maximum": 2200,
			"compensation_currency": "USD",
			"compensation_frequency": "two_weeks",
			"location": {"name": "Leeds"}
		}]}`,
	})

	postings, errs := drain(Pinpoint(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	must.NotNil(t, postings[0].Compensation)
	test.Eq(t, internal.PeriodUnknown, postings[0].Compensation.Period)
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
