package enrich_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/shoenig/test/must"
)

// errBoardDown stands in for a source failure in the stream tests.
var errBoardDown = errors.New("board is down")

// header renders a table header line for the fixture filesystems below.
func header(columns []string) string { return strings.Join(columns, "\t") + "\n" }

// row renders one record in the order of columns.
func row(columns []string, values map[string]string) string {
	fields := make([]string, len(columns))
	for i, column := range columns {
		fields[i] = values[column]
	}

	return strings.Join(fields, "\t") + "\n"
}

// tableFS builds a filesystem shaped like the embedded one.
func tableFS(employers, manual, wages string) fstest.MapFS {
	return fstest.MapFS{
		"data/employers.tsv": {Data: []byte(employers)},
		"data/manual.tsv":    {Data: []byte(manual)},
		"data/wages.tsv":     {Data: []byte(wages)},
	}
}

// employerRow is a well-formed employer record with the given overrides.
func employerRow(overrides map[string]string) string {
	values := map[string]string{
		"platform":         "greenhouse",
		"key":              "acme",
		"company":          "acme",
		"legal_name":       "Acme Industries, Inc.",
		"cik":              "0000000001",
		"sic":              "7372",
		"industry":         "Services-Prepackaged Software",
		"public":           "true",
		"employees":        "4000",
		"match_method":     "edgar-exact-name",
		"match_confidence": "high",
		"data_sources":     "sec-edgar",
		"retrieved":        "2026-07-27",
	}

	for key, value := range overrides {
		values[key] = value
	}

	return row(enrich.EmployerColumns(), values)
}

// TestEmbeddedTablesParse is the golden test for what this binary actually
// ships.
//
// A corrupted regeneration, a stray tab in a company name, or a hand-edited
// manual.tsv with a missing column would otherwise present at runtime as every
// company on earth being unknown, which is indistinguishable from the honest
// answer. This fails in CI instead.
//
// It deliberately does not assert a row count. The committed table is empty
// today because no generator run against the live sources has happened, and a
// test demanding rows would invite somebody to invent them.
func TestEmbeddedTablesParse(t *testing.T) {
	t.Parallel()

	table, err := enrich.Default()
	must.NoError(t, err)
	must.NotNil(t, table)

	for _, employer := range table.All() {
		must.False(t, employer.Source.IsZero(),
			must.Sprintf("employer %q has no source identity to join on", employer.Company))
		must.NotEq(t, enrich.MethodUnknown, employer.Match.Method,
			must.Sprintf("employer %q does not record how it was matched", employer.Company))
		must.NotEq(t, enrich.ConfidenceUnknown, employer.Match.Confidence,
			must.Sprintf("employer %q does not record how far to trust its match", employer.Company))
	}
}

// TestDefaultIsCachedAndShared checks the sync.OnceValues loader hands back the
// same immutable table rather than reparsing per call, since Attach is on the
// hot path of a 473,404-posting crawl.
func TestDefaultIsCachedAndShared(t *testing.T) {
	t.Parallel()

	first, err := enrich.Default()
	must.NoError(t, err)

	second, err := enrich.Default()
	must.NoError(t, err)

	must.Eq(t, first.Len(), second.Len())
}

// TestManualRowsOverrideGeneratedOnes covers the correction mechanism. The
// generator rewrites employers.tsv wholesale, so a fix made there would be
// reverted by the next refresh; manual.tsv is what survives.
func TestManualRowsOverrideGeneratedOnes(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()

	table, err := enrich.LoadFS(tableFS(
		header(columns)+employerRow(nil),
		header(columns)+employerRow(map[string]string{
			"legal_name":       "Acme Industries Holdings, Inc.",
			"match_method":     "manual",
			"match_confidence": "high",
		}),
		header(enrich.WageColumns()),
	))
	must.NoError(t, err)

	employer, ok := table.For(internal.PostingSource{Platform: "greenhouse", Key: "acme"})
	must.True(t, ok)
	must.Eq(t, "Acme Industries Holdings, Inc.", employer.LegalName)
	must.Eq(t, enrich.MethodManual, employer.Match.Method)
	must.Eq(t, 1, table.Len(), must.Sprint("an override must replace the row, not add a second one"))
}

// TestDuplicateRowsAreRefused: two rows for one source is ambiguous, and
// whichever won would be arbitrary.
func TestDuplicateRowsAreRefused(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()

	_, err := enrich.LoadFS(tableFS(
		header(columns)+employerRow(nil)+employerRow(map[string]string{"legal_name": "Someone Else, Inc."}),
		header(columns),
		header(enrich.WageColumns()),
	))

	must.ErrorContains(t, err, "already mapped")
}

// TestRowsWithoutProvenanceAreRefused: a row nobody can audit is a row nobody
// can correct, and the hand-edited file is exactly where one appears.
func TestRowsWithoutProvenanceAreRefused(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()

	for name, overrides := range map[string]map[string]string{
		"no method":     {"match_method": ""},
		"no confidence": {"match_confidence": ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := enrich.LoadFS(tableFS(
				header(columns)+employerRow(overrides),
				header(columns),
				header(enrich.WageColumns()),
			))

			must.ErrorContains(t, err, "every row must record how it was matched")
		})
	}
}

// TestHeaderMismatchIsRefused covers the schema drift a half-finished column
// change produces. Silently ignoring an unexpected column would ship an empty
// value for every company with no error anywhere.
func TestHeaderMismatchIsRefused(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()
	typo := strings.Replace(header(columns), "employees", "employes", 1)

	_, err := enrich.LoadFS(tableFS(typo, header(columns), header(enrich.WageColumns())))

	must.ErrorContains(t, err, "missing [employees]")
	must.ErrorContains(t, err, "unexpected [employes]")
}

// TestLoaderToleratesCarriageReturnsAndComments is insurance against
// .gitattributes.
//
// The repository sets `* text=auto`, so a committed .tsv is LF-normalized in the
// index and can arrive with CRLF line endings in a Windows working tree. A
// parser that kept the \r would attach it to the last column of every row, and
// the embedded table would work everywhere except on one platform. Comments are
// tested alongside because the tables carry their own provenance in them.
func TestLoaderToleratesCarriageReturnsAndComments(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()

	content := "# generated 2026-07-27\n#\n\n" + header(columns) + employerRow(nil)
	crlf := strings.ReplaceAll(content, "\n", "\r\n")

	table, err := enrich.LoadFS(tableFS(crlf, header(columns), header(enrich.WageColumns())))
	must.NoError(t, err)

	employer, ok := table.For(internal.PostingSource{Platform: "greenhouse", Key: "acme"})
	must.True(t, ok)
	must.Eq(t, "2026-07-27", employer.Match.RetrievedAt, must.Sprint(
		"a carriage return was left on the last column"))
}

// TestJoinKeyFoldsCase: a reviewer retyping a slug in a different case must not
// silently detach a row from its source.
func TestJoinKeyFoldsCase(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()

	table, err := enrich.LoadFS(tableFS(
		header(columns)+employerRow(map[string]string{"key": "PaloAltoNetworks2", "company": "PaloAltoNetworks2"}),
		header(columns),
		header(enrich.WageColumns()),
	))
	must.NoError(t, err)

	_, ok := table.For(internal.PostingSource{Platform: "Greenhouse", Key: "paloaltonetworks2"})
	must.True(t, ok)
}

// TestWageBenchmarksAttachToTheirEmployer covers the framework the wage
// generator will slot into, including the case that must be dropped rather than
// guessed: a benchmark for a source nobody matched.
func TestWageBenchmarksAttachToTheirEmployer(t *testing.T) {
	t.Parallel()

	columns := enrich.EmployerColumns()
	wageCols := enrich.WageColumns()

	wages := header(wageCols) +
		row(wageCols, map[string]string{
			"platform": "greenhouse", "key": "acme", "soc": "15-1212",
			"occupation": "Information Security Analysts", "area": "CA", "source": "oflc",
			"n": "42", "p25": "150000", "p50": "175000", "p75": "205000", "as_of": "FY2025",
		}) +
		row(wageCols, map[string]string{
			"platform": "lever", "key": "nobody", "soc": "15-1212", "area": "US", "source": "oews",
			"n": "9", "p50": "120000", "as_of": "2024-05",
		})

	table, err := enrich.LoadFS(tableFS(header(columns)+employerRow(nil), header(columns), wages))
	must.NoError(t, err)

	employer, ok := table.For(internal.PostingSource{Platform: "greenhouse", Key: "acme"})
	must.True(t, ok)
	must.Len(t, 1, employer.WageBenchmarks)
	must.Eq(t, enrich.WageSourceOFLC, employer.WageBenchmarks[0].Source)
	must.Eq(t, 175000.0, employer.WageBenchmarks[0].P50)

	_, ok = table.For(internal.PostingSource{Platform: "lever", Key: "nobody"})
	must.False(t, ok, must.Sprint("a wage row must not conjure an employer nobody matched"))
}

// TestEmployerTableRoundTrips ties the writer the generator uses to the reader
// the binary uses. They used to be able to drift, and a generator emitting a
// table the loader rejects is a failure that only appears after a live run.
func TestEmployerTableRoundTrips(t *testing.T) {
	t.Parallel()

	private := false

	original := []*enrich.Employer{
		employer("greenhouse", "acme", "Acme Industries, Inc."),
		{
			Source:       internal.PostingSource{Platform: "workday", Key: "https://beta.wd1.myworkdayjobs.com/careers"},
			Company:      "beta",
			LegalName:    "Beta Robotics GmbH",
			Public:       &private,
			Employees:    120,
			Founded:      2018,
			Headquarters: "Berlin",
			Parent:       "Beta Holding",
			WikidataID:   "Q1",
			Match: enrich.Match{
				Method:      enrich.MethodManual,
				Confidence:  enrich.ConfidenceHigh,
				DataSources: []string{"wikidata"},
				RetrievedAt: "2026-07-27",
			},
		},
	}

	var buf bytes.Buffer
	must.NoError(t, enrich.WriteEmployers(&buf, original, "test table"))

	table, err := enrich.LoadFS(tableFS(buf.String(), header(enrich.EmployerColumns()), header(enrich.WageColumns())))
	must.NoError(t, err)
	must.Eq(t, 2, table.Len())

	// Sorted output, so the diff of a monthly regeneration shows what changed
	// rather than what moved.
	all := table.All()
	must.Eq(t, "greenhouse", all[0].Source.Platform)
	must.Eq(t, "workday", all[1].Source.Platform)

	must.Eq(t, original[1].Employees, all[1].Employees)
	must.NotNil(t, all[1].Public)
	must.False(t, *all[1].Public, must.Sprint("a known-private employer must not round-trip as unknown"))
	must.Eq(t, []string{"wikidata"}, all[1].Match.DataSources)
}

// TestWriteEmployersRejectsTabsInValues: a tab inside a value would shift every
// later column and the row would still parse, which is the corruption mode a
// text table has.
func TestWriteEmployersRejectsTabsInValues(t *testing.T) {
	t.Parallel()

	broken := employer("greenhouse", "acme", "Acme\tIndustries")

	err := enrich.WriteEmployers(&bytes.Buffer{}, []*enrich.Employer{broken})
	must.ErrorContains(t, err, "contains a tab or newline")
}

// TestCoverageCountsWhatIsKnown: the number that must be printed next to any
// answer this table gives.
func TestCoverageCountsWhatIsKnown(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))

	matched, total := table.Coverage([]internal.PostingSource{
		{Platform: "greenhouse", Key: "acme"},
		{Platform: "lever", Key: "beta"},
		{},
	})

	must.Eq(t, 1, matched)
	must.Eq(t, 2, total, must.Sprint("a source with no identity is not a source that could have matched"))
}
