package services

import (
	"slices"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestAshbyHQ(t *testing.T) {
	testSingle(t, "openai", AshbyHQ)
}

func TestAshbyHQ_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(AshbyHQCompanies), AshbyHQ)
}

// ashbyEnrichedBoard is one response in the shape Ashby already serves for the
// URL this adapter already fetches. Nothing here needs a second request.
//
// The two postings differ in every way that matters to the decoder: the first
// publishes structured pay *and* a different figure in its prose, so which
// source won is provable; the second publishes no structured pay, no
// workplaceType and no publishedAt, so the fallbacks are exercised.
const ashbyEnrichedBoard = `{
	"jobs": [
		{
			"id": "6f4e2e10-1a2b-4c3d-9e8f-000000000001",
			"jobUrl": "https://jobs.ashbyhq.com/acme/1",
			"title": "Staff Security Engineer",
			"location": "New York",
			"department": "Engineering",
			"team": "Platform Security",
			"employmentType": "FullTime",
			"workplaceType": "Hybrid",
			"publishedAt": "2026-04-30T16:21:55.393+00:00",
			"isRemote": false,
			"descriptionPlain": "About the role. The base salary range for this position is $90,000 - $110,000 per year.",
			"compensation": {
				"compensationTierSummary": "$236K – $290K • Offers Equity",
				"compensationTiers": [
					{"components": [
						{"compensationType": "Salary", "interval": "1 YEAR",
						 "currencyCode": "USD", "minValue": 236000, "maxValue": 290000}
					]}
				]
			}
		},
		{
			"id": "6f4e2e10-1a2b-4c3d-9e8f-000000000002",
			"jobUrl": "https://jobs.ashbyhq.com/acme/2",
			"title": "Security Intern",
			"location": "Anywhere",
			"department": "Engineering",
			"employmentType": "Intern",
			"publishedAt": "",
			"isRemote": true,
			"descriptionPlain": "The pay range for this internship is $30.00 - $45.00 per hour.",
			"compensation": {"compensationTierSummary": "", "compensationTiers": []}
		}
	]
}`

// TestAshbyHQDecodesTheFieldsAlreadyOnTheWire covers the enrichment fields.
//
// Every one of them was in the response the adapter already downloaded and was
// discarded by encoding/json because the struct did not name it, on all 418
// Ashby sources. The test asserts the *normalized* values rather than Ashby's
// spellings, because passing "FullTime" through raw is what would push
// per-platform vocabulary into internal/filter.go.
func TestAshbyHQDecodesTheFieldsAlreadyOnTheWire(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"api.ashbyhq.com": ashbyEnrichedBoard})

	postings, errs := drain(AshbyHQ(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	staff, intern := postings[0], postings[1]

	test.Eq(t, "Engineering", staff.Department)
	test.Eq(t, "Platform Security", staff.Team)
	test.Eq(t, internal.EmploymentTypeFullTime, staff.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeHybrid, staff.WorkplaceType)
	test.Eq(t, "6f4e2e10-1a2b-4c3d-9e8f-000000000001", staff.ExternalID)

	// Milliseconds and a numeric zone both survive, and the instant is stored in
	// UTC so it is comparable with a Lever or Greenhouse timestamp.
	test.Eq(t, time.Date(2026, time.April, 30, 16, 21, 55, 393_000_000, time.UTC), staff.PostedAt)
	test.Eq(t, "UTC", staff.PostedAt.Location().String())

	// platform+key is the stable integration ID, and it used to die inside the
	// crawler: a posting carried only a short company name.
	test.Eq(t, internal.PostingSource{Platform: "ashby", Key: "acme"}, staff.Source)

	// "Intern" is Ashby's spelling of an internship, and the board published no
	// workplaceType for this one, so the structured isRemote boolean answers.
	test.Eq(t, internal.EmploymentTypeInternship, intern.EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, intern.WorkplaceType)

	// A missing publishedAt stays the zero time rather than becoming "now",
	// which is what keeps --posted-since from silently matching undated postings.
	test.True(t, intern.PostedAt.IsZero())
}

// TestAshbyHQPrefersEmployerPayOverProse is the provenance guard.
//
// Prose extraction is free on Ashby because the description is already on the
// wire, but a figure read out of a sentence must never be mistaken for one the
// employer published in a structured field, and must never overwrite one.
func TestAshbyHQPrefersEmployerPayOverProse(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"api.ashbyhq.com": ashbyEnrichedBoard})

	postings, errs := drain(AshbyHQ(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// The description of this posting also states a range, a deliberately
	// different one. The employer's own numbers must be the ones published.
	employer := postings[0].Compensation
	must.NotNil(t, employer)
	test.Eq(t, internal.ProvenanceEmployer, employer.Provenance)
	test.Eq(t, 236000.0, employer.Min)
	test.Eq(t, 290000.0, employer.Max)

	// This posting publishes no structured pay at all, so the prose figure fills
	// an otherwise empty field, labelled as prose.
	prose := postings[1].Compensation
	must.NotNil(t, prose)
	test.Eq(t, internal.ProvenanceDescription, prose.Provenance)
	test.Eq(t, 30.0, prose.Min)
	test.Eq(t, 45.0, prose.Max)
	test.Eq(t, internal.PeriodHour, prose.Period)
}

// TestAshbyHQMakesOneRequestPerBoard guards the cost of the above: reading the
// description must stay free. It is a field of the response already fetched, so
// using it may not add a request, a URL parameter, or a per-posting fetch.
func TestAshbyHQMakesOneRequestPerBoard(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{"api.ashbyhq.com": ashbyEnrichedBoard})

	postings, errs := drain(AshbyHQ(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)
	must.Len(t, 1, transport.requests)
}

func TestAshbyWorkplaceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		isRemote bool
		want     internal.WorkplaceType
	}{
		{name: "explicit remote", raw: "Remote", want: internal.WorkplaceTypeRemote},
		{name: "explicit hybrid", raw: "Hybrid", want: internal.WorkplaceTypeHybrid},
		{name: "explicit onsite", raw: "Onsite", want: internal.WorkplaceTypeOnsite},

		// The structured field wins over the boolean when both are present, and
		// the two really do disagree: a hybrid role is not remote.
		{name: "hybrid beats the remote flag", raw: "Hybrid", isRemote: true, want: internal.WorkplaceTypeHybrid},

		// isRemote true is the board stating the role is remote.
		{name: "flag fills an absent field", raw: "", isRemote: true, want: internal.WorkplaceTypeRemote},

		// isRemote false only says "not fully remote". Inventing an office
		// requirement from it would be a guess, and unknown is not onsite.
		{name: "flag false says nothing", raw: "", isRemote: false, want: internal.WorkplaceTypeUnknown},

		// An unrecognised spelling leaves the field empty rather than guessing.
		{name: "unrecognised spelling", raw: "Flexible", want: internal.WorkplaceTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tt.want, ashbyWorkplaceType(tt.raw, tt.isRemote))
		})
	}
}

func TestAshbyPublishedAt(t *testing.T) {
	t.Parallel()

	tests := map[string]time.Time{
		// Ashby's own documented shape: milliseconds and a numeric zone.
		"2021-04-30T16:21:55.393+00:00": time.Date(2021, time.April, 30, 16, 21, 55, 393_000_000, time.UTC),

		// A non-UTC zone is converted rather than kept, so postings from two
		// boards compare as instants.
		"2026-01-31T20:00:00-05:00": time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC),

		"2026-01-31T20:00:00Z": time.Date(2026, time.January, 31, 20, 0, 0, 0, time.UTC),

		// Anything unreadable is left as the zero time: one odd posting must not
		// cost a board its other postings, and an absent date is visibly absent.
		"":           {},
		"yesterday":  {},
		"2026-01-31": {},
	}

	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			got := ashbyPublishedAt(raw)

			test.Eq(t, want, got)

			if !want.IsZero() {
				test.Eq(t, "UTC", got.Location().String())
			}
		})
	}
}
