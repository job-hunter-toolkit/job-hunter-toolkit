package internal_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// coreJSONFields are the posting fields whose JSON encoding is a committed
// contract: always present, never omitted, in this order.
var coreJSONFields = map[string]string{
	"Company":  "company",
	"URL":      "url",
	"Title":    "title",
	"Location": "location",
}

// legacyOptionalJSONFields are the two fields that shipped before the schema
// grew. Their names are as frozen as the core four; what differs is that they
// were already omitted when absent, and must stay that way — emitting
// "compensation":null on the ~99% of postings that disclose no pay would be as
// much of a format change as dropping a key.
var legacyOptionalJSONFields = map[string]string{
	"Compensation": "compensation",
	"Remote":       "remote",
}

func TestJobPostingCoreJSONIsUnchanged(t *testing.T) {
	t.Parallel()

	// A golden test, not a round trip. The output of a crawl is newline-delimited
	// JSON that people pipe into jq, so the encoding of a posting carrying no
	// enrichment is a contract with everyone who already has a pipeline. Enriching
	// the schema must be invisible to them, byte for byte.
	posting := &internal.JobPosting{
		Company:  "acme",
		URL:      "https://example.test/1",
		Title:    "Security Engineer",
		Location: "Remote",
	}

	data, err := json.Marshal(posting)
	must.NoError(t, err)

	const want = `{"company":"acme","url":"https://example.test/1","title":"Security Engineer","location":"Remote"}`

	must.Eq(t, want, string(data), must.Sprint("the pre-enrichment JSON contract changed"))
}

func TestJobPostingCoreFieldsSurviveBeingEmpty(t *testing.T) {
	t.Parallel()

	// The four core fields must never gain omitempty. A consumer running
	// `jq -r .location` on a posting from a board that published no location has
	// to see an empty string, not a missing key: the first is a value, the second
	// is a crash in someone's script.
	data, err := json.Marshal(&internal.JobPosting{})
	must.NoError(t, err)

	must.Eq(t, `{"company":"","url":"","title":"","location":""}`, string(data))
}

func TestJobPostingTagsKeepTheWireContract(t *testing.T) {
	t.Parallel()

	// Enforced by reflection rather than by review because several agents add
	// fields to this struct. A new field without omitempty/omitzero would appear
	// on all ~473,000 postings of a crawl as an empty value, which is both a
	// silent format change for existing consumers and a large amount of stdout
	// spent saying nothing.
	typ := reflect.TypeFor[internal.JobPosting]()

	for i := range typ.NumField() {
		field := typ.Field(i)

		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")

		if wantName, core := coreJSONFields[field.Name]; core {
			test.Eq(t, wantName, name, test.Sprintf("core field %s changed its JSON name", field.Name))
			test.Eq(t, "", options,
				test.Sprintf("core field %s gained JSON options %q; it must always be emitted", field.Name, options))

			continue
		}

		if wantName, legacy := legacyOptionalJSONFields[field.Name]; legacy {
			test.Eq(t, wantName, name, test.Sprintf("field %s changed its JSON name", field.Name))
		}

		test.NotEq(t, "", name, test.Sprintf("field %s has no JSON name", field.Name))

		set := strings.Split(options, ",")
		omitted := slices.Contains(set, "omitempty") || slices.Contains(set, "omitzero")

		test.True(t, omitted, test.Sprintf(
			"field %s must be tagged omitempty or omitzero so postings without it encode as they did before",
			field.Name))
	}
}

func TestJobPostingEnrichmentRoundTrips(t *testing.T) {
	t.Parallel()

	posted := time.Date(2026, 4, 30, 16, 21, 55, 393000000, time.UTC)

	posting := &internal.JobPosting{
		Company:        "acme",
		URL:            "https://example.test/1",
		Title:          "Security Engineer",
		Location:       "Austin, TX",
		Department:     "Engineering",
		Team:           "Platform",
		EmploymentType: internal.EmploymentTypeFullTime,
		WorkplaceType:  internal.WorkplaceTypeHybrid,
		Seniority:      "Senior",
		PostedAt:       posted,
		UpdatedAt:      posted.Add(72 * time.Hour),
		RequisitionID:  "JR0012345",
		ExternalID:     "4019283",
		Source:         internal.PostingSource{Platform: "greenhouse", Key: "acmecorp"},
	}

	data, err := json.Marshal(posting)
	must.NoError(t, err)

	var decoded internal.JobPosting

	must.NoError(t, json.Unmarshal(data, &decoded))
	must.Eq(t, *posting, decoded, must.Sprint("a posting did not survive a JSON round trip"))

	// Timestamps have to survive exactly, because they are compared rather than
	// displayed: Filter.PostedSince asks whether one instant precedes another.
	test.True(t, decoded.PostedAt.Equal(posted))
	test.StrContains(t, string(data), `"posted_at":"2026-04-30T16:21:55.393Z"`)
	test.StrContains(t, string(data), `"source":{"platform":"greenhouse","key":"acmecorp"}`)
}

func TestPostingSourceIsOmittedWhenAbsent(t *testing.T) {
	t.Parallel()

	// omitzero on a struct field relies on this IsZero method; without it every
	// posting from an adapter that has not been migrated yet would carry an empty
	// "source":{} object.
	test.True(t, internal.PostingSource{}.IsZero())
	test.False(t, internal.PostingSource{Platform: "lever"}.IsZero())
	test.False(t, internal.PostingSource{Key: "acme"}.IsZero())

	data, err := json.Marshal(&internal.JobPosting{Company: "acme"})
	must.NoError(t, err)
	must.StrNotContains(t, string(data), "source")
}

func TestNormalizeEmploymentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want internal.EmploymentType
		ok   bool
	}{
		// The spellings below are the ones the platform audit found in real
		// responses, one per adapter that publishes the field.
		{name: "ashby employmentType", raw: "FullTime", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "ashby intern", raw: "Intern", want: internal.EmploymentTypeInternship, ok: true},
		{name: "lever commitment", raw: "Fulltime", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "lever hyphenated commitment", raw: "Full-time", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "lever part time", raw: "Part-time", want: internal.EmploymentTypePartTime, ok: true},
		{name: "bamboohr employmentStatusLabel", raw: "Full-Time", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "bamboohr contractor", raw: "Contractor", want: internal.EmploymentTypeContract, ok: true},
		{name: "smartrecruiters typeOfEmployment", raw: "Full-time", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "peopleforce details segment", raw: "Full Time Position", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "peopleforce internship", raw: "Internship", want: internal.EmploymentTypeInternship, ok: true},
		{name: "workday time type", raw: "Full time", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "schema.org via jibe", raw: "FULL_TIME", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "schema.org contractor", raw: "CONTRACTOR", want: internal.EmploymentTypeContract, ok: true},
		{name: "schema.org volunteer", raw: "VOLUNTEER", want: internal.EmploymentTypeVolunteer, ok: true},
		{name: "phenom type", raw: "Regular Full-Time", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "padded intern posting", raw: "Intern (Summer 2026)", want: internal.EmploymentTypeInternship, ok: true},
		{name: "seasonal is temporary", raw: "Seasonal", want: internal.EmploymentTypeTemporary, ok: true},
		{name: "abbreviation", raw: "FT", want: internal.EmploymentTypeFullTime, ok: true},
		{name: "temp abbreviation", raw: "Temp", want: internal.EmploymentTypeTemporary, ok: true},

		// A canonical value must survive a second pass, so an adapter that
		// normalizes twice cannot lose the field.
		{name: "already canonical", raw: "full_time", want: internal.EmploymentTypeFullTime, ok: true},

		// Unrecognised values leave the field empty. Guessing would put a wrong
		// answer where a filter cannot tell it from a right one, while an absent
		// field is visibly absent.
		{name: "empty", raw: "", ok: false},
		{name: "blank", raw: "   ", ok: false},
		{name: "lever unspecified", raw: "unspecified", ok: false},
		{name: "schema.org other", raw: "OTHER", ok: false},
		{name: "tenure is not hours", raw: "Permanent", ok: false},
		{name: "workday worker type", raw: "Regular", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := internal.NormalizeEmploymentType(tt.raw)

			must.Eq(t, tt.ok, ok, must.Sprintf("NormalizeEmploymentType(%q) recognised = %v", tt.raw, ok))
			must.Eq(t, tt.want, got, must.Sprintf("NormalizeEmploymentType(%q)", tt.raw))
		})
	}
}

func TestNormalizeWorkplaceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want internal.WorkplaceType
		ok   bool
	}{
		{name: "ashby workplaceType", raw: "Remote", want: internal.WorkplaceTypeRemote, ok: true},
		{name: "ashby onsite", raw: "Onsite", want: internal.WorkplaceTypeOnsite, ok: true},
		{name: "lever workplaceType", raw: "on-site", want: internal.WorkplaceTypeOnsite, ok: true},
		{name: "lever hybrid", raw: "hybrid", want: internal.WorkplaceTypeHybrid, ok: true},
		{name: "rippling screaming case", raw: "REMOTE", want: internal.WorkplaceTypeRemote, ok: true},
		{name: "rippling on site", raw: "ON_SITE", want: internal.WorkplaceTypeOnsite, ok: true},
		{name: "bamboohr locationType", raw: "onsite", want: internal.WorkplaceTypeOnsite, ok: true},
		{name: "gem locationType", raw: "IN_OFFICE", want: internal.WorkplaceTypeOnsite, ok: true},
		{name: "peopleforce segment", raw: "Any - Remote", want: internal.WorkplaceTypeRemote, ok: true},
		{name: "peopleforce office", raw: "Office", want: internal.WorkplaceTypeOnsite, ok: true},
		{name: "work from home", raw: "Work From Home", want: internal.WorkplaceTypeRemote, ok: true},
		{name: "wfh", raw: "WFH", want: internal.WorkplaceTypeRemote, ok: true},

		// Boards write both words when they mean hybrid; the office requirement is
		// the constraining half of the pair, so it wins.
		{name: "hybrid remote", raw: "Hybrid Remote", want: internal.WorkplaceTypeHybrid, ok: true},
		{name: "remote slash hybrid", raw: "Remote/Hybrid", want: internal.WorkplaceTypeHybrid, ok: true},

		// The reverse ordering trap: an office listing that permits remote work is
		// remote-eligible, not onsite-only.
		{name: "office with remote option", raw: "Office - Remote optional", want: internal.WorkplaceTypeRemote, ok: true},

		{name: "already canonical", raw: "onsite", want: internal.WorkplaceTypeOnsite, ok: true},

		// Lever's default. Reading it as onsite would invent an office requirement
		// the employer declined to state.
		{name: "lever unspecified", raw: "unspecified", ok: false},
		{name: "empty", raw: "", ok: false},
		{name: "unknown vocabulary", raw: "Flexible", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := internal.NormalizeWorkplaceType(tt.raw)

			must.Eq(t, tt.ok, ok, must.Sprintf("NormalizeWorkplaceType(%q) recognised = %v", tt.raw, ok))
			must.Eq(t, tt.want, got, must.Sprintf("NormalizeWorkplaceType(%q)", tt.raw))
		})
	}
}

func TestVocabularyValuesAreNormalizable(t *testing.T) {
	t.Parallel()

	// The canonical lists back the CLI's flag help and its validation, so every
	// value shown to a user must be one the normalizer accepts. Otherwise the help
	// text could advertise a value the flag then rejects.
	for _, want := range internal.EmploymentTypeValues() {
		got, ok := internal.NormalizeEmploymentType(string(want))

		test.True(t, ok, test.Sprintf("canonical employment type %q is not recognised", want))
		test.Eq(t, want, got)
	}

	for _, want := range internal.WorkplaceTypeValues() {
		got, ok := internal.NormalizeWorkplaceType(string(want))

		test.True(t, ok, test.Sprintf("canonical workplace type %q is not recognised", want))
		test.Eq(t, want, got)
	}
}
