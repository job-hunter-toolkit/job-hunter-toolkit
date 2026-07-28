package services

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestBreezy(t *testing.T) {
	testSingle(t, "matroid", Breezy)
}

func TestBreezy_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	testMultipleParallel(t, slices.Values(BreezyCompanies), Breezy)
}

// TestBreezyParsesPositions covers the field mapping against a hand-written
// board: the object-shaped "type", the nullable "department", the location the
// board renders itself, and the employer-published pay string.
func TestBreezyParsesPositions(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.breezy.hr/json": `[
			{
				"id": "764f8a9923e3",
				"friendly_id": "764f8a9923e3-security-engineer",
				"name": "  Security Engineer  ",
				"url": "https://acme.breezy.hr/p/764f8a9923e3-security-engineer",
				"published_date": "2026-07-10T19:56:44.147Z",
				"type": {"id": "fullTime", "name": "Full-Time"},
				"department": "Engineering",
				"salary": "$170,000 – $300,000 / year",
				"location": {
					"country": {"name": "United States", "id": "US"},
					"city": "Palo Alto",
					"name": "Palo Alto, CA",
					"is_remote": false
				},
				"locations": [{"name": "Palo Alto, CA", "is_remote": false}]
			},
			{
				"id": "2cc745ec3a62",
				"name": "Support Specialist",
				"url": "https://acme.breezy.hr/p/2cc745ec3a62-support-specialist",
				"published_date": "2026-06-03T19:53:43.737Z",
				"type": "Part-Time",
				"department": null,
				"salary": "",
				"location": {"country": {"name": "Philippines"}, "city": null, "name": "Philippines", "is_remote": true}
			},
			{
				"id": "9e8be090952c",
				"name": "Warehouse Associate",
				"url": "https://acme.breezy.hr/p/9e8be090952c-warehouse-associate",
				"type": {"id": "contract", "name": "Contract"},
				"department": {"name": "Operations"},
				"salary": "Competitive",
				"location": {"country": {"name": "United Kingdom"}, "city": "Leeds", "is_remote": false}
			},
			{
				"id": "no-link",
				"name": "No link",
				"url": "",
				"location": {"name": "Nowhere"}
			}
		]`,
	})

	postings, errs := drain(Breezy(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	security := postings[0]

	test.Eq(t, "acme", security.Company)
	test.Eq(t, "Security Engineer", security.Title)
	test.Eq(t, "https://acme.breezy.hr/p/764f8a9923e3-security-engineer", security.URL)
	test.Eq(t, "Palo Alto, CA", security.Location)
	test.Eq(t, "Engineering", security.Department)
	test.Eq(t, "764f8a9923e3", security.ExternalID)
	test.Eq(t, internal.EmploymentTypeFullTime, security.EmploymentType)
	test.Eq(t, internal.PostingSource{Platform: "breezy", Key: "acme"}, security.Source)
	test.Eq(t, time.Date(2026, time.July, 10, 19, 56, 44, 147000000, time.UTC), security.PostedAt)

	// Not marked remote, and Breezy has no value meaning "office required", so
	// the field stays absent rather than being stored as false.
	test.Nil(t, security.Remote)
	test.Eq(t, internal.WorkplaceTypeUnknown, security.WorkplaceType)

	must.NotNil(t, security.Compensation)
	test.Eq(t, 170000.0, security.Compensation.Min)
	test.Eq(t, 300000.0, security.Compensation.Max)
	test.Eq(t, "USD", security.Compensation.Currency)
	test.Eq(t, "$170,000 – $300,000 / year", security.Compensation.Summary)
	test.Eq(t, internal.ProvenanceEmployer, security.Compensation.Provenance)

	support := postings[1]

	// "type" as a bare string is the shape the survey documents; it still reads.
	test.Eq(t, internal.EmploymentTypePartTime, support.EmploymentType)
	test.Eq(t, "", support.Department)
	test.Eq(t, "Philippines", support.Location)
	test.Nil(t, support.Compensation)

	must.NotNil(t, support.Remote)
	test.True(t, *support.Remote)
	test.Eq(t, internal.WorkplaceTypeRemote, support.WorkplaceType)

	warehouse := postings[2]

	// No "name" on the location, so it is rebuilt from city and country.
	test.Eq(t, "Leeds, United Kingdom", warehouse.Location)
	test.Eq(t, "Operations", warehouse.Department)
	test.Eq(t, internal.EmploymentTypeContract, warehouse.EmploymentType)

	// "Competitive" is a pay string with no figure this project will stand
	// behind, so nothing is published for it.
	test.Nil(t, warehouse.Compensation)

	// A position with no https URL is dropped rather than published with a
	// broken link.
	test.True(t, warehouse.PostedAt.IsZero())
}

// TestBreezyReadsTheSpacedPayPeriod covers the unit Breezy renders after a
// spaced slash, which no marker in internal/compensation_text.go matches.
//
// Every case here is a real string shape from the live platform. Before
// [breezyPeriodWording] existed, none of them set a period and all of them were
// left to the magnitude heuristic, which calls a figure at or under 250 hourly
// and anything above it annual. The day rate is the one that did visible harm:
// it was published as $83,200-$124,800 a year with employer provenance.
func TestBreezyReadsTheSpacedPayPeriod(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		salary string
		period internal.Period
		min    float64
		max    float64
	}{
		{"$170,000 – $300,000 / year", internal.PeriodYear, 170000, 300000},
		{"$20 – $25 / hour", internal.PeriodHour, 20, 25},
		{"$5,000 – $8,000 / month", internal.PeriodMonth, 5000, 8000},
		{"$1,200 – $1,800 / week", internal.PeriodWeek, 1200, 1800},
		{"$400 – $600 / day", internal.PeriodDay, 400, 600},
	} {
		t.Run(tc.salary, func(t *testing.T) {
			t.Parallel()

			compensation := breezyCompensation(tc.salary)

			must.NotNil(t, compensation)
			test.Eq(t, tc.period, compensation.Period)
			test.Eq(t, tc.min, compensation.Min)
			test.Eq(t, tc.max, compensation.Max)

			// The board's own rendering is what the employer published, so it is
			// kept verbatim even though the parser saw a rewritten copy.
			test.Eq(t, tc.salary, compensation.Summary)
		})
	}

	// A day rate whose annualized floor falls under the parser's plausible-wage
	// bound is now dropped rather than republished as an hourly rate. Reporting
	// no pay is the honest outcome; reporting $124,800 was not.
	test.Nil(t, breezyCompensation("$40 – $60 / day"))
}

// TestBreezyAcceptsTheLegacyObjectShape covers the older
// {"company":..,"positions":[..]} response.
//
// docs/research/ats-platform-survey.md records that the endpoint rolled from
// that shape to a flat array. Measured on 2026-07-28, all 1,895 boards that
// answered used the array — the object was not seen once — so this case exists
// entirely to keep a roll-back from turning the whole platform into silently
// empty sources.
func TestBreezyAcceptsTheLegacyObjectShape(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.breezy.hr/json": `{
			"company": {"name": "Acme"},
			"positions": [
				{
					"_id": "abc123",
					"name": "Staff Engineer",
					"url": "https://acme.breezy.hr/p/abc123-staff-engineer",
					"location": {"name": "Remote", "is_remote": true}
				}
			]
		}`,
	})

	postings, errs := drain(Breezy(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	// The id arrives under its old name in this shape.
	test.Eq(t, "abc123", postings[0].ExternalID)
	test.Eq(t, "Remote", postings[0].Location)
}

// TestBreezyEmptyBoardIsNotAnError is the case docs/adding-a-source.md is
// explicit about: a company that is not hiring today is not a broken source.
func TestBreezyEmptyBoardIsNotAnError(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.breezy.hr/json": `[]`,
	})

	postings, errs := drain(Breezy(t.Context(), client, "acme"))

	test.SliceEmpty(t, errs)
	test.SliceEmpty(t, postings)
}

// TestBreezyReportsAShapeChange checks the guard against the silently-empty
// failure: positions that decode but carry none of the fields this adapter
// needs must be an error, not zero postings.
func TestBreezyReportsAShapeChange(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"renamed fields": `[{"title": "Security Engineer", "apply_url": "https://acme.breezy.hr/p/1"}]`,
		"neither shape":  `{"jobs": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, _ := fixtureClient(map[string]string{"acme.breezy.hr/json": body})

			postings, errs := drain(Breezy(t.Context(), client, "acme"))

			test.SliceEmpty(t, postings)
			must.Len(t, 1, errs)
			test.StrContains(t, errs[0].Error(), "acme")
		})
	}
}

// TestBreezyReportsHTTPFailures covers a non-200 and a body that is not JSON.
//
// The non-200 case is the live behaviour of a slug with no board: Breezy answers
// 302 to https://breezy.hr/, whose 403 is what the crawl actually sees.
func TestBreezyReportsHTTPFailures(t *testing.T) {
	t.Parallel()

	t.Run("non-200", func(t *testing.T) {
		t.Parallel()

		client, transport := fixtureClient(map[string]string{"acme.breezy.hr/json": ``})
		transport.status = 403

		postings, errs := drain(Breezy(t.Context(), client, "acme"))

		test.SliceEmpty(t, postings)
		must.Len(t, 1, errs)
		test.StrContains(t, errs[0].Error(), "acme")
	})

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()

		client, _ := fixtureClient(map[string]string{"acme.breezy.hr/json": `[{"name":`})

		postings, errs := drain(Breezy(t.Context(), client, "acme"))

		test.SliceEmpty(t, postings)
		must.Len(t, 1, errs)
		test.StrContains(t, errs[0].Error(), "acme")
	})
}

// breezyFixture reads a response captured verbatim from a live Breezy board.
//
// Unlike the other captures under testdata these are byte-for-byte what the
// board answered: the whole response is the list this adapter reads, there is no
// description field to strip, and both boards are small enough to keep whole.
func breezyFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// TestBreezyParsesACapturedLiveBoard is the fixture that decides whether this
// adapter reads Breezy, as opposed to reading the shape a document said Breezy
// has. The body is https://alt-legal.breezy.hr/json as captured on 2026-07-28.
//
// What the capture establishes, and what the hand-written fixture above cannot:
//
//   - "type" really is an object on a live board.
//     docs/research/ats-platform-survey.md lists it as a plain string, and an
//     adapter that modelled it as a Go string would fail to decode every
//     position of every tenant on the platform — not silently, but completely.
//   - "location.city" really is null on a remote position while "location.name"
//     carries a country. The survey documents city as a string.
//   - the pay string is not always dollars. This board publishes
//     "₱1,368 – ₱1,308,000 / year", a range whose ends are three orders of
//     magnitude apart, and the range-ratio guard in
//     [internal.ParseCompensationFromText] is what stops it being published.
//   - "department" really is JSON null on some positions and a string on others.
func TestBreezyParsesACapturedLiveBoard(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"alt-legal.breezy.hr/json": breezyFixture(t, "breezy_altlegal_positions.json"),
	})

	postings, errs := drain(Breezy(t.Context(), client, "alt-legal"))

	must.SliceEmpty(t, errs)
	must.Len(t, 4, postings)

	first := postings[0]

	test.Eq(t, "alt-legal", first.Company)
	test.Eq(t, "Customer Success Manager", first.Title)
	test.Eq(t, "https://alt-legal.breezy.hr/p/33b788b22d82-customer-success-manager", first.URL)
	test.Eq(t, "United States", first.Location)
	test.Eq(t, "Success", first.Department)
	test.Eq(t, "33b788b22d82", first.ExternalID)
	test.Eq(t, internal.EmploymentTypeFullTime, first.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, first.WorkplaceType)
	test.Eq(t, internal.PostingSource{Platform: "breezy", Key: "alt-legal"}, first.Source)

	must.NotNil(t, first.Remote)
	test.True(t, *first.Remote)

	// A peso range from 1,368 to 1,308,000 is not a salary range; publishing
	// either end of it would be worse than publishing nothing.
	test.Nil(t, postings[1].Compensation)
	test.Eq(t, "", postings[1].Department)

	operations := postings[2]

	must.NotNil(t, operations.Compensation)
	test.Eq(t, 38000.0, operations.Compensation.Min)
	test.Eq(t, 45000.0, operations.Compensation.Max)
	test.Eq(t, internal.ProvenanceEmployer, operations.Compensation.Provenance)
}

// TestBreezyParsesACapturedLiveBoardWithStreetAddresses is a second capture,
// https://matroid.breezy.hr/json on 2026-07-28, kept because it exercises the
// parts of the shape alt-legal has none of: a nested streetAddress object with
// its own components array, a null city on a position that also has a name, a
// second department value, and an hourly pay string alongside annual ones.
func TestBreezyParsesACapturedLiveBoardWithStreetAddresses(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"matroid.breezy.hr/json": breezyFixture(t, "breezy_matroid_positions.json"),
	})

	postings, errs := drain(Breezy(t.Context(), client, "matroid"))

	must.SliceEmpty(t, errs)
	must.Len(t, 10, postings)

	first := postings[0]

	test.Eq(t, "Deep Learning Engineer", first.Title)
	test.Eq(t, "Palo Alto, CA", first.Location)
	test.Eq(t, "Engineering", first.Department)

	must.NotNil(t, first.Compensation)
	test.Eq(t, 170000.0, first.Compensation.Min)
	test.Eq(t, 300000.0, first.Compensation.Max)

	// Every posting on a live board carries a title, an https URL and a posted
	// date; a capture where that stopped being true would be the shape change
	// this adapter is meant to notice.
	for _, posting := range postings {
		test.NotEq(t, "", posting.Title)
		test.StrHasPrefix(t, "https://matroid.breezy.hr/p/", posting.URL)
		test.False(t, posting.PostedAt.IsZero())
	}
}

// TestBreezyRegisteredCompaniesComeFromTheCandidateFile keeps the registered
// list traceable: every slug in it must appear in the researched candidate file,
// so a slug cannot be added without the provenance that file records.
func TestBreezyRegisteredCompaniesComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "breezy_slugs.txt")

	must.Greater(t, 1_000, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(BreezyCompanies))

	for _, slug := range BreezyCompanies {
		test.False(t, seen[slug], test.Sprintf("company %q is registered twice", slug))
		seen[slug] = true

		test.True(t, candidates[slug], test.Sprintf("registered company %q is not in testdata/candidates/breezy_slugs.txt", slug))
	}

	test.Less(t, len(candidates), len(BreezyCompanies), test.Sprint("the registered list should stay a subset of the candidates"))
}
