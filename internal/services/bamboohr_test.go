package services

import (
	"slices"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestBambooHR(t *testing.T) {
	testSingle(t, "zerofox", BambooHR)
}

func TestBambooHR_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(BambooHRCompanies), BambooHR)
}

// TestBambooHRReadsTheFieldsItAlreadyDecoded is a regression test.
//
// departmentLabel, employmentStatusLabel, isRemote, locationType and the whole
// atsLocation object have been decoded into bambooInfo since this adapter was
// written and were never read: downloaded on every request for all 55 tenants,
// then dropped by the yield. Recovering them costs no request, no byte and no
// new host.
func TestBambooHRReadsTheFieldsItAlreadyDecoded(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.bamboohr.com": `{
			"meta": {"totalCount": 3},
			"result": [
				{
					"id": "101",
					"jobOpeningName": "Security Engineer",
					"departmentLabel": "Engineering",
					"employmentStatusLabel": "Full-Time",
					"location": {"city": "Austin", "state": "TX"},
					"isRemote": false,
					"locationType": "hybrid"
				},
				{
					"id": "102",
					"jobOpeningName": "Support Lead",
					"departmentLabel": "Customer Success",
					"employmentStatusLabel": "PT",
					"location": {"city": "", "state": ""},
					"atsLocation": {"city": "Berlin", "state": null, "province": null, "country": "Germany"},
					"isRemote": "yes",
					"locationType": ""
				},
				{
					"id": "103",
					"jobOpeningName": "Volunteer Coordinator",
					"departmentLabel": "",
					"employmentStatusLabel": "Seasonal Helper",
					"location": {"city": "", "state": ""},
					"isRemote": null,
					"locationType": "Remote"
				}
			]
		}`,
	})

	postings, errs := drain(BambooHR(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	test.Eq(t, "Engineering", postings[0].Department)
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, internal.WorkplaceTypeHybrid, postings[0].WorkplaceType)
	test.Eq(t, "101", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: bambooHRPlatform, Key: "acme"}, postings[0].Source)

	// isRemote is false here, and false is a statement the board made, so it is
	// recorded — unlike an absent flag, which must stay nil.
	must.NotNil(t, postings[0].Remote)
	test.False(t, *postings[0].Remote)

	// The flat city/state pair is where the location has always come from, and
	// is unchanged for every posting that has one.
	test.Eq(t, "Austin, TX", postings[0].Location)

	// A tenant that sends isRemote as a string is still read. Modelling that
	// field as a bool would fail the decode and take the whole tenant with it.
	test.Eq(t, "Customer Success", postings[1].Department)
	test.Eq(t, internal.EmploymentTypePartTime, postings[1].EmploymentType)
	must.NotNil(t, postings[1].Remote)
	test.True(t, *postings[1].Remote)

	// atsLocation carries a real place for a posting whose flat pair is empty.
	// Those used to be labelled "remote" outright, which for a Berlin office is
	// an invented fact.
	test.Eq(t, "Berlin, Germany", postings[1].Location)

	// locationType is the structured answer and wins; isRemote is absent here.
	test.Eq(t, internal.WorkplaceTypeRemote, postings[2].WorkplaceType)
	test.Nil(t, postings[2].Remote)
	test.Eq(t, internal.EmploymentTypeTemporary, postings[2].EmploymentType)

	// Nothing published a location at all, so the old placeholder still stands.
	test.Eq(t, "remote", postings[2].Location)
}

// TestBambooHRLeavesUnrecognisedVocabularyEmpty pins the rule every adapter
// follows: a value the canonical vocabulary does not recognise is left empty
// rather than guessed at. A wrong employment type is invisible to whoever
// filters on it; an absent one is not.
func TestBambooHRLeavesUnrecognisedVocabularyEmpty(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.bamboohr.com": `{
			"meta": {"totalCount": 1},
			"result": [{
				"id": "1",
				"jobOpeningName": "Staff Engineer",
				"employmentStatusLabel": "Regular",
				"location": {"city": "Austin", "state": "TX"},
				"locationType": "Flexible"
			}]
		}`,
	})

	postings, errs := drain(BambooHR(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, internal.EmploymentTypeUnknown, postings[0].EmploymentType)
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[0].WorkplaceType)
}

func TestAnyBool(t *testing.T) {
	t.Parallel()

	// The second result is what keeps "the board did not say" apart from "the
	// board said no": only a published value reaches JobPosting.Remote at all,
	// and a nil Remote is what lets the location-text heuristic run.
	tests := []struct {
		name      string
		value     any
		want      bool
		published bool
	}{
		{name: "bool true", value: true, want: true, published: true},
		{name: "bool false", value: false, published: true},
		{name: "string true", value: "true", want: true, published: true},
		{name: "string yes", value: " Yes ", want: true, published: true},
		{name: "string no", value: "no", published: true},
		{name: "number", value: float64(1), want: true, published: true},
		{name: "null", value: nil},
		{name: "empty string", value: ""},
		{name: "object", value: map[string]any{"a": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, published := anyBool(tt.value)

			test.Eq(t, tt.published, published)
			test.Eq(t, tt.want, got)
		})
	}
}

func TestAnyText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "string", value: "  Berlin  ", want: "Berlin"},
		{name: "null", value: nil},
		// Every JSON number decodes as float64, and %v would print an id as
		// "1.01e+02"; a requisition number has to survive that round trip.
		{name: "number", value: float64(101), want: "101"},
		{name: "bool", value: true, want: "true"},
		{name: "list", value: []any{"Engineering", "Platform"}, want: "Engineering"},
		{name: "empty list", value: []any{}},
		// Never Go's %v spelling of a map, which would put "map[]" in a posting.
		{name: "object", value: map[string]any{"label": "Engineering"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tt.want, anyText(tt.value))
		})
	}
}
