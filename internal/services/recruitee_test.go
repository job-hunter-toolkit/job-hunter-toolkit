package services

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRecruitee(t *testing.T) {
	testSingle(t, "bunq", Recruitee)
}

func TestRecruitee_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	testMultipleParallel(t, slices.Values(RecruiteeCompanies), Recruitee)
}

// TestRecruiteeParsesOffers covers the enrichment this platform publishes and
// the two shapes its numeric fields arrive in. The first offer writes its id and
// salary as numbers, the second writes both as strings; both are what the
// reference implementation this adapter was written against had to cope with,
// and a Go float64 field would have failed the whole response on one of them.
func TestRecruiteeParsesOffers(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.recruitee.com/api/offers/": `{
			"offers": [
				{
					"id": 1234567,
					"slug": "security-engineer",
					"title": "  Security Engineer  ",
					"careers_url": "https://acme.recruitee.com/o/security-engineer",
					"department": "Engineering",
					"employment_type_code": "fulltime",
					"experience_code": "senior",
					"remote": true,
					"city": "Amsterdam",
					"country": "Netherlands",
					"location": "Amsterdam, Netherlands",
					"published_at": "2026-05-28 20:36:05 UTC",
					"updated_at": "2026-06-02 08:00:00 UTC",
					"description": "<p>ignored</p>",
					"salary": {"min": 70000, "max": 90000, "currency": "eur", "period": "year"}
				},
				{
					"id": "7654321",
					"slug": "support-agent",
					"title": "Support Agent",
					"department": "Customer Success",
					"employment_type_code": "parttime",
					"remote": false,
					"city": "Rotterdam",
					"country": "Netherlands",
					"published_at": "2026-05-01 09:00:00 UTC",
					"salary": {"min": "2400", "max": "3000", "currency": "EUR", "period": "monthly"}
				},
				{
					"id": 999,
					"title": "No link anywhere",
					"department": "Operations"
				}
			]
		}`,
	})

	postings, errs := drain(Recruitee(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	engineer := postings[0]

	test.Eq(t, "Security Engineer", engineer.Title)
	test.Eq(t, "acme", engineer.Company)
	test.Eq(t, "https://acme.recruitee.com/o/security-engineer", engineer.URL)
	test.Eq(t, "Amsterdam, Netherlands", engineer.Location)
	test.Eq(t, "Engineering", engineer.Department)
	test.Eq(t, "senior", engineer.Seniority)
	test.Eq(t, internal.EmploymentTypeFullTime, engineer.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, engineer.WorkplaceType)
	test.Eq(t, "1234567", engineer.ExternalID)
	test.Eq(t, internal.PostingSource{Platform: "recruitee", Key: "acme"}, engineer.Source)

	must.NotNil(t, engineer.Remote)
	test.True(t, *engineer.Remote)

	// "2026-05-28 20:36:05 UTC" is not a format time.Parse knows without being
	// told, and it is stored as an instant in UTC.
	test.Eq(t, "2026-05-28T20:36:05Z", engineer.PostedAt.Format(time.RFC3339))
	test.Eq(t, "2026-06-02T08:00:00Z", engineer.UpdatedAt.Format(time.RFC3339))

	must.NotNil(t, engineer.Compensation)
	test.Eq(t, 70000.0, engineer.Compensation.Min)
	test.Eq(t, 90000.0, engineer.Compensation.Max)
	test.Eq(t, "EUR", engineer.Compensation.Currency)
	test.Eq(t, internal.PeriodYear, engineer.Compensation.Period)
	test.Eq(t, internal.ProvenanceEmployer, engineer.Compensation.Provenance)

	agent := postings[1]

	// No careers_url, so the public posting page is rebuilt from the slug.
	test.Eq(t, "https://acme.recruitee.com/o/support-agent", agent.URL)
	test.Eq(t, "Rotterdam, Netherlands", agent.Location)
	test.Eq(t, "7654321", agent.ExternalID)
	test.Eq(t, internal.EmploymentTypePartTime, agent.EmploymentType)

	must.NotNil(t, agent.Compensation)
	test.Eq(t, 2400.0, agent.Compensation.Min)
	test.Eq(t, internal.PeriodMonth, agent.Compensation.Period)

	// remote=false is not a statement that the role is onsite, so it leaves both
	// remote fields empty and lets IsRemote's location-text fallback run.
	test.Nil(t, agent.Remote)
	test.Eq(t, internal.WorkplaceTypeUnknown, agent.WorkplaceType)
}

// recruiteeFixture reads a response captured from a live Recruitee board.
//
// The capture under testdata is what the board answered with the fields this
// adapter never decodes removed — description, requirements,
// sharing_description, open_questions, dynamic_fields, translations and the
// locations array, together the great majority of the bytes. Every other key,
// and every value, is the board's own.
func recruiteeFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// TestRecruiteeParsesACapturedLiveBoard is the fixture that decides whether this
// adapter reads Recruitee, as opposed to reading the shape a document said
// Recruitee has. The body is https://germanzeroev.recruitee.com/api/offers/ as
// captured on 2026-07-28.
//
// What the capture establishes, and what the hand-written fixture above cannot:
//
//   - "hybrid" and "on_site" exist alongside "remote".
//     docs/research/ats-platform-survey.md documents only remote(bool), and this
//     adapter shipped mapping every non-remote posting to
//     [internal.WorkplaceTypeUnknown]. All three keys are present on all 9,832
//     postings measured across the platform, and exactly one is set on 8,841 of
//     them — so reading only "remote" threw away a real structured answer for
//     90% of Recruitee.
//   - they are independent booleans, not a three-state enum. The last offer here
//     sets hybrid AND on_site, which is why [recruiteeWorkplaceType] answers
//     unknown for anything but a single set flag instead of picking a winner.
//   - salary.min and salary.max arrive as JSON STRINGS ("3500"), not numbers,
//     which is exactly the polymorphism recruiteeScalar exists for; a float64
//     field would fail the decode and take the whole board with it.
//   - salary.period is real. The survey lists salary{min,max,currency} and
//     nothing else; 3,373 live postings publish pay and their periods are
//     "month" (2,348), "year" (431) and "hour" (409).
//   - published_at really is "2026-07-27 15:33:54 UTC", which no [time.Parse]
//     layout handles without being told.
//   - "department" is null on real postings, so the field has to tolerate it.
func TestRecruiteeParsesACapturedLiveBoard(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"germanzeroev.recruitee.com/api/offers/": recruiteeFixture(t, "recruitee_germanzeroev_offers.json"),
	})

	postings, errs := drain(Recruitee(t.Context(), client, "germanzeroev"))

	must.SliceEmpty(t, errs)
	must.Len(t, 5, postings)

	assistant := postings[0]

	test.Eq(t, "germanzeroev", assistant.Company)
	test.Eq(t, "2691426", assistant.ExternalID)
	test.Eq(t, "Berlin, Berlin, Deutschland", assistant.Location)
	test.Eq(t, "entry_level", assistant.Seniority)
	test.Eq(t, "2026-07-27T15:33:54Z", assistant.PostedAt.Format(time.RFC3339))

	// parttime_fixed_term is the compound spelling the live platform uses; the
	// survey's vocabulary has only "parttime".
	test.Eq(t, internal.EmploymentTypePartTime, assistant.EmploymentType)

	// hybrid alone: a positive statement that the role is not fully remote.
	test.Eq(t, internal.WorkplaceTypeHybrid, assistant.WorkplaceType)
	must.NotNil(t, assistant.Remote)
	test.False(t, *assistant.Remote)

	must.NotNil(t, assistant.Compensation)
	test.Eq(t, 3500.0, assistant.Compensation.Min)
	test.Eq(t, 3800.0, assistant.Compensation.Max)
	test.Eq(t, "EUR", assistant.Compensation.Currency)
	test.Eq(t, internal.PeriodMonth, assistant.Compensation.Period)
	test.Eq(t, internal.ProvenanceEmployer, assistant.Compensation.Provenance)

	// fulltime_fixed_term, and a range with only an upper bound.
	lead := postings[1]

	test.Eq(t, internal.EmploymentTypeFullTime, lead.EmploymentType)
	must.NotNil(t, lead.Compensation)
	test.Eq(t, 0.0, lead.Compensation.Min)
	test.Eq(t, 5000.0, lead.Compensation.Max)

	// remote alone.
	internship := postings[2]

	test.Eq(t, internal.EmploymentTypeInternship, internship.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, internship.WorkplaceType)
	test.Eq(t, "Homeoffice", internship.Location)
	test.Nil(t, internship.Compensation)

	must.NotNil(t, internship.Remote)
	test.True(t, *internship.Remote)

	// hybrid AND on_site: the employer named two arrangements, so this adapter
	// names none and leaves IsRemote's location-text fallback in charge.
	volunteer := postings[4]

	test.Eq(t, internal.WorkplaceTypeUnknown, volunteer.WorkplaceType)
	test.Nil(t, volunteer.Remote)
}

// TestRecruiteeIgnoresSalaryWithoutFigures keeps --has-pay honest: a salary
// object holding only a currency discloses nothing, and publishing it as a pay
// range would make a pay filter match postings that named no number.
func TestRecruiteeIgnoresSalaryWithoutFigures(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"recruitee.com": `{"offers": [{
			"id": 1,
			"slug": "a",
			"title": "A",
			"salary": {"currency": "EUR", "min": null, "max": "negotiable"}
		}]}`,
	})

	postings, errs := drain(Recruitee(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	test.Nil(t, postings[0].Compensation)
}

func TestRecruiteeReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"recruitee.com": `{}`},
		status: http.StatusNotFound,
	}

	postings, errs := drain(Recruitee(t.Context(), &http.Client{Transport: transport}, "gone"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "gone")
}

func TestRecruiteeReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"recruitee.com": `{"offers": [`})

	postings, errs := drain(Recruitee(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestRecruiteeReportsAResponseWithNoOffers covers the silently-empty failure
// this project treats as its worst: a 200 that is not the offers feed decodes
// into an empty list, and reporting that as "this company is not hiring" would
// hide the break. The nil-versus-empty distinction is why the field is a
// pointer.
func TestRecruiteeReportsAResponseWithNoOffers(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"recruitee.com": `{"data": {"offers": []}}`})

	postings, errs := drain(Recruitee(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "offers")
}

// TestRecruiteeReportsEmptyBoardWithoutError is the other half of that
// distinction: an empty offers array is a real answer from a company that is not
// hiring today, and docs/adding-a-source.md is explicit that is not a failure.
func TestRecruiteeReportsEmptyBoardWithoutError(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"recruitee.com": `{"offers": []}`})

	postings, errs := drain(Recruitee(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

// TestRecruiteeReportsOffersThatYieldNoPostings covers a renamed field: the
// response is full of offers and not one produces a posting, which no live board
// does.
func TestRecruiteeReportsOffersThatYieldNoPostings(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"recruitee.com": `{"offers": [
			{"id": 1, "name": "Security Engineer", "path": "/o/security-engineer"},
			{"id": 2, "name": "Support Agent", "path": "/o/support-agent"}
		]}`,
	})

	postings, errs := drain(Recruitee(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "2 offers decoded")
}

// TestRecruiteeCompaniesComeFromTheCandidateFile keeps the registered list
// honest: every slug in it is one a research pass actually recorded, and the
// registered set stays a small staged subset rather than the whole unprobed
// harvest.
func TestRecruiteeCompaniesComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "recruitee_slugs.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(RecruiteeCompanies))

	for _, slug := range RecruiteeCompanies {
		test.False(t, seen[slug], test.Sprintf("company %q is registered twice", slug))
		seen[slug] = true

		test.True(t, candidates[slug], test.Sprintf("registered company %q is not in testdata/candidates/recruitee_slugs.txt", slug))
	}

	test.Less(t, len(candidates), len(RecruiteeCompanies), test.Sprint("the registered list should stay a subset of the candidates"))
}
