package enrich_test

import (
	"reflect"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/shoenig/test/must"
)

// applyFilter runs a filter over one posting and reports whether it survived.
func applyFilter(t *testing.T, filter enrich.Filter, table *enrich.Table, job *internal.JobPosting) bool {
	t.Helper()

	found, failed := collect(t, filter.Apply(table.Attach(jobsFrom(job))))
	must.SliceEmpty(t, failed)

	return len(found) == 1
}

// TestEnrichFilterFieldsAreWiredIn is the same guard internal/filter.go carries,
// for the same reason.
//
// [enrich.Filter.Apply] returns its input untouched when IsZero reports true,
// and IsZero enumerates its fields by hand. A field added to the struct and to
// the CLI but forgotten in IsZero makes its flag match the entire crawl: no
// error, no log line, just a wrong answer at 473,404 postings of scale. The
// project has already been bitten by exactly this once, in the other filter.
func TestEnrichFilterFieldsAreWiredIn(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[enrich.Filter]()

	for i := range typ.NumField() {
		field := typ.Field(i)

		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()

			must.True(t, field.IsExported(), must.Sprintf(
				"Filter.%s is unexported, so this reflection guard cannot set it", field.Name))

			value := reflect.New(typ).Elem()
			value.Field(i).Set(constrainingValue(t, field.Type))

			filter, ok := value.Interface().(enrich.Filter)
			must.True(t, ok)

			must.False(t, filter.IsZero(), must.Sprintf(
				"Filter.IsZero() ignores %s, so a filter setting only that field would pass straight "+
					"through Apply and silently match every posting; add %s to IsZero", field.Name, field.Name))

			must.False(t, filter.Match(&enrich.Posting{JobPosting: &internal.JobPosting{}}), must.Sprintf(
				"Filter.Match() ignores %s: a posting whose employer is unknown satisfied a constraint "+
					"about that employer. Wire %s into Match", field.Name, field.Name))
		})
	}
}

// constrainingValue builds a value for a filter field that is guaranteed to
// constrain something, whichever type a future field turns out to have.
func constrainingValue(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()

	value := reflect.New(typ).Elem()

	switch typ.Kind() {
	case reflect.Bool:
		value.SetBool(true)
	case reflect.String:
		value.SetString("constraining")
	case reflect.Int, reflect.Int64:
		value.SetInt(1)
	case reflect.Float64:
		value.SetFloat(1)
	case reflect.Slice:
		return reflect.Append(reflect.MakeSlice(typ, 0, 1), constrainingValue(t, typ.Elem()))
	default:
		t.Fatalf("no constraining value known for filter field type %s; teach constrainingValue about it, "+
			"and check Filter.IsZero and Filter.Match handle it", typ)
	}

	return value
}

// TestFilterPrivateIsNotTheComplementOfPublic is the distinction the whole
// tri-state exists for.
//
// Not finding an SEC filing is not evidence that a company is privately held,
// it is evidence that nobody resolved it. `--private` returning every unmatched
// startup would be a confident wrong answer about ~2,000 companies.
func TestFilterPrivateIsNotTheComplementOfPublic(t *testing.T) {
	t.Parallel()

	var (
		public  = true
		private = false
	)

	table := enrich.NewTable(
		&enrich.Employer{
			Source: internal.PostingSource{Platform: "greenhouse", Key: "listed"},
			Public: &public,
			Match:  enrich.Match{Method: enrich.MethodEDGARExactName, Confidence: enrich.ConfidenceHigh},
		},
		&enrich.Employer{
			Source: internal.PostingSource{Platform: "greenhouse", Key: "held"},
			Public: &private,
			Match:  enrich.Match{Method: enrich.MethodManual, Confidence: enrich.ConfidenceHigh},
		},
		&enrich.Employer{
			Source: internal.PostingSource{Platform: "greenhouse", Key: "unchecked"},
			Match:  enrich.Match{Method: enrich.MethodManual, Confidence: enrich.ConfidenceHigh},
		},
	)

	for name, want := range map[string]map[string]bool{
		"public":  {"listed": true, "held": false, "unchecked": false, "absent": false},
		"private": {"listed": false, "held": true, "unchecked": false, "absent": false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			filter := enrich.Filter{Public: name == "public", Private: name == "private"}

			for key, matches := range want {
				must.Eq(t, matches, applyFilter(t, filter, table, posting("greenhouse", key, key)),
					must.Sprintf("--%s on the %q employer", name, key))
			}
		})
	}
}

// TestFilterIndustryMatchesCodeOrDescription: EDGAR classifies with a
// four-digit code and people talk in words, so both have to work.
func TestFilterIndustryMatchesCodeOrDescription(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))

	for _, term := range []string{"software", "SOFTWARE", "prepackaged", "7372"} {
		must.True(t, applyFilter(t, enrich.Filter{Industries: []string{term}}, table, posting("greenhouse", "acme", "acme")),
			must.Sprintf("--industry %q", term))
	}

	must.False(t, applyFilter(t, enrich.Filter{Industries: []string{"pharmaceutical"}}, table, posting("greenhouse", "acme", "acme")))
}

// TestFilterMinEmployeesExcludesUnknownHeadcount follows the precedent
// internal.Filter.MinAnnual sets: a floor cannot be applied to an unknown.
func TestFilterMinEmployeesExcludesUnknownHeadcount(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(
		employer("greenhouse", "acme", "Acme Industries, Inc."),
		&enrich.Employer{
			Source: internal.PostingSource{Platform: "greenhouse", Key: "unsized"},
			Match:  enrich.Match{Method: enrich.MethodManual, Confidence: enrich.ConfidenceHigh},
		},
	)

	filter := enrich.Filter{MinEmployees: 1000}

	must.True(t, applyFilter(t, filter, table, posting("greenhouse", "acme", "acme")))
	must.False(t, applyFilter(t, filter, table, posting("greenhouse", "unsized", "unsized")))
}

// TestFilterKnownEmployer is the "only show me what I have context for" flag.
func TestFilterKnownEmployer(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))
	filter := enrich.Filter{Known: true}

	must.True(t, applyFilter(t, filter, table, posting("greenhouse", "acme", "acme")))
	must.False(t, applyFilter(t, filter, table, posting("greenhouse", "beta", "beta")))
}

// TestZeroFilterIsPassThrough: the default `postings` run must not pay for a
// filter it did not ask for, and must not drop a posting either.
func TestZeroFilterIsPassThrough(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable()

	must.True(t, enrich.Filter{}.IsZero())
	must.True(t, applyFilter(t, enrich.Filter{}, table, posting("greenhouse", "unknown", "unknown")))
	must.True(t, enrich.Filter{Industries: []string{"  "}}.IsZero(),
		must.Sprint("a list of blank terms is no constraint, matching internal/filter.go"))
}

// TestFilterApplyPassesErrorsThrough: an employer filter must not turn a broken
// source into a silently missing one.
func TestFilterApplyPassesErrorsThrough(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))

	found, failed := collect(t, enrich.Filter{Known: true}.Apply(table.Attach(jobsFrom(
		posting("greenhouse", "acme", "acme"),
		errBoardDown,
		posting("greenhouse", "beta", "beta"),
	))))

	must.Len(t, 1, found)
	must.Len(t, 1, failed)
	must.ErrorIs(t, failed[0], errBoardDown)
}
