package services

import (
	"net/http"
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
