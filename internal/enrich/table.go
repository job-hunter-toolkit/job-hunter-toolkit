package enrich

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// data holds the committed enrichment tables.
//
// Compiled in rather than kept in an on-disk cache or downloaded on first use,
// because docs/architecture-roadmap.md requires the CLI to work with "no
// required state". A table the binary carries cannot be missing, cannot be stale
// in a way the version does not describe, and cannot make a query hit the
// network.
//
//go:embed data/employers.tsv data/manual.tsv data/wages.tsv
var data embed.FS

// The table file names, which are also what error messages say.
const (
	employersFile = "data/employers.tsv"
	manualFile    = "data/manual.tsv"
	wagesFile     = "data/wages.tsv"
)

// employerColumns is the employer table's schema, in file order.
//
// company is carried purely for review: it is the string a person recognises
// when reading a diff, and a mapping file nobody can read is a mapping file
// nobody corrects.
var employerColumns = []string{
	"platform", "key", "company",
	"legal_name", "cik", "ticker", "exchange",
	"sic", "industry", "public",
	"employees", "founded", "headquarters", "parent", "wikidata_id",
	"match_method", "match_confidence", "data_sources", "retrieved",
}

// wageColumns is the wage benchmark table's schema, in file order.
var wageColumns = []string{
	"platform", "key", "soc", "occupation", "area", "source",
	"n", "p25", "p50", "p75", "as_of",
}

// Table is the reviewed mapping from a crawled source to what is known about
// its employer. It is immutable once built and safe for concurrent use.
type Table struct {
	// employers is keyed by joinKey, which lowercases platform and key. The
	// separator mirrors the \x00 join internal.Dedupe already uses for its
	// composite key.
	employers map[string]*Employer
}

// defaultTable is the embedded table, parsed once.
//
// Parsed lazily rather than in an init function so a binary whose user never
// asks for enrichment never pays for it, and so a corrupt table is an error at
// the point of use with a message, not a panic during process start.
var defaultTable = sync.OnceValues(func() (*Table, error) {
	return LoadFS(data)
})

// Default returns the table compiled into this binary.
//
// It returns an error rather than an empty table when the embedded data cannot
// be parsed, because the two are not the same thing and the difference matters:
// no rows is the honest state of a company nobody has resolved, while an
// unparseable table is a bug that would otherwise present as every company on
// earth being unknown.
func Default() (*Table, error) { return defaultTable() }

// LoadFS builds a table from a filesystem laid out like the embedded one.
//
// It exists so the loader can be tested against fixtures, and so a future
// --enrich-table flag could point at a locally regenerated file without the
// loader growing a second code path.
func LoadFS(fsys fs.FS) (*Table, error) {
	employers, err := readTable(fsys, employersFile, employerColumns)
	if err != nil {
		return nil, err
	}

	// Manual rows are applied over generated ones, and that ordering is the
	// entire correction mechanism. The generator rewrites employers.tsv wholesale
	// on every run, so a human fix made there would be silently reverted by the
	// next refresh; a fix made in manual.tsv survives, and survives visibly,
	// because the file contains nothing except decisions a person made.
	manual, err := readTable(fsys, manualFile, employerColumns)
	if err != nil {
		return nil, err
	}

	rows := make(map[string]*Employer, len(employers)+len(manual))
	maps.Copy(rows, employers)
	maps.Copy(rows, manual)

	if err := attachWages(fsys, rows); err != nil {
		return nil, err
	}

	return &Table{employers: rows}, nil
}

// readTable parses one employer table into rows keyed by their join key.
func readTable(fsys fs.FS, name string, columns []string) (map[string]*Employer, error) {
	file, err := fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("opening enrichment table %s: %w", name, err)
	}
	defer file.Close()

	return parseEmployers(name, file, columns)
}

// parseEmployers reads an employer table, refusing anything ambiguous.
func parseEmployers(name string, r io.Reader, columns []string) (map[string]*Employer, error) {
	table, err := readTSV(name, r, columns)
	if err != nil {
		return nil, err
	}

	rows := make(map[string]*Employer, len(table.rows))

	for _, row := range table.rows {
		employer, err := employerFromRow(row)
		if err != nil {
			return nil, err
		}

		key := joinKey(employer.Source)

		// Two rows for one source is not a conflict to resolve by picking one:
		// whichever the map ended up with would be arbitrary, and the wrong half
		// of a duplicate is exactly the plausible-looking wrong answer this
		// package exists to avoid. Refuse, and name the line so the reviewer can
		// delete the right one.
		if previous, ok := rows[key]; ok {
			return nil, fmt.Errorf("reading %s line %d: %s/%s is already mapped to %q; one source may have only one employer row",
				name, row.line, employer.Source.Platform, employer.Source.Key, previous.LegalName)
		}

		rows[key] = employer
	}

	return rows, nil
}

// employerFromRow converts one parsed record into an [Employer].
func employerFromRow(row tsvRow) (*Employer, error) {
	source := internal.PostingSource{
		Platform: row.str("platform"),
		Key:      row.str("key"),
	}

	// A row with no join key cannot be looked up by anything, so it is dead
	// weight that would silently inflate a coverage count.
	if source.IsZero() {
		return nil, fmt.Errorf("%s line %d: platform and key are both empty, so this row can never be joined to a posting",
			row.table, row.line)
	}

	employer := &Employer{
		Source:       source,
		Company:      row.str("company"),
		LegalName:    row.str("legal_name"),
		CIK:          row.str("cik"),
		Ticker:       row.str("ticker"),
		Exchange:     row.str("exchange"),
		SIC:          row.str("sic"),
		Industry:     row.str("industry"),
		Headquarters: row.str("headquarters"),
		Parent:       row.str("parent"),
		WikidataID:   row.str("wikidata_id"),
		Match: Match{
			Method:      Method(row.str("match_method")),
			Confidence:  Confidence(row.str("match_confidence")),
			DataSources: row.list("data_sources"),
			RetrievedAt: row.str("retrieved"),
		},
	}

	var err error

	if employer.Public, err = row.boolPtr("public"); err != nil {
		return nil, err
	}

	if employer.Employees, err = row.int("employees"); err != nil {
		return nil, err
	}

	if employer.Founded, err = row.int("founded"); err != nil {
		return nil, err
	}

	// A row with no provenance is a row nobody can audit. The generator always
	// writes both, so this only fires on a hand-edited row, which is precisely
	// the row that most needs to say where it came from.
	if employer.Match.Method == MethodUnknown || employer.Match.Confidence == ConfidenceUnknown {
		return nil, fmt.Errorf("%s line %d: %s/%s has match_method=%q match_confidence=%q; every row must record how it was matched and how far to trust it",
			row.table, row.line, source.Platform, source.Key,
			employer.Match.Method, employer.Match.Confidence)
	}

	return employer, nil
}

// attachWages reads the wage benchmark table and hangs each row off the employer
// it belongs to.
//
// A benchmark for a source with no employer row is dropped rather than
// synthesising an employer: the wage tables are aggregated from DOL and BLS
// files keyed by employer name, so a benchmark with no reviewed match is exactly
// the case where the name-based join could be wrong.
func attachWages(fsys fs.FS, rows map[string]*Employer) error {
	file, err := fsys.Open(wagesFile)
	if err != nil {
		return fmt.Errorf("opening enrichment table %s: %w", wagesFile, err)
	}
	defer file.Close()

	table, err := readTSV(wagesFile, file, wageColumns)
	if err != nil {
		return err
	}

	for _, row := range table.rows {
		source := internal.PostingSource{Platform: row.str("platform"), Key: row.str("key")}

		employer, ok := rows[joinKey(source)]
		if !ok {
			continue
		}

		benchmark := WageBenchmark{
			SOC:        row.str("soc"),
			Occupation: row.str("occupation"),
			Area:       row.str("area"),
			Source:     WageSource(row.str("source")),
			AsOf:       row.str("as_of"),
		}

		if benchmark.N, err = row.int("n"); err != nil {
			return err
		}

		if benchmark.P25, err = row.float("p25"); err != nil {
			return err
		}

		if benchmark.P50, err = row.float("p50"); err != nil {
			return err
		}

		if benchmark.P75, err = row.float("p75"); err != nil {
			return err
		}

		employer.WageBenchmarks = append(employer.WageBenchmarks, benchmark)
	}

	return nil
}

// NewTable builds a table from employer records, for tests and for callers
// assembling a table in memory. Later records win, matching the file overlay
// order.
func NewTable(employers ...*Employer) *Table {
	rows := make(map[string]*Employer, len(employers))

	for _, employer := range employers {
		if employer == nil || employer.Source.IsZero() {
			continue
		}

		rows[joinKey(employer.Source)] = employer
	}

	return &Table{employers: rows}
}

// For returns what is known about the employer behind a source.
//
// A nil table reports nothing found rather than panicking, so a caller that
// could not load the table still runs: no enrichment is the documented default
// answer, and it is a strictly better outcome than refusing to print postings.
func (t *Table) For(source internal.PostingSource) (*Employer, bool) {
	if t == nil || len(t.employers) == 0 || source.IsZero() {
		return nil, false
	}

	employer, ok := t.employers[joinKey(source)]

	return employer, ok
}

// Len reports how many sources the table has a reviewed row for.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}

	return len(t.employers)
}

// All returns every employer row, ordered by platform and key so output is
// stable across runs. Map iteration order would make `company --json` produce a
// different byte stream every invocation, which breaks diffing a snapshot
// against yesterday's.
func (t *Table) All() []*Employer {
	if t == nil {
		return nil
	}

	employers := slices.Collect(maps.Values(t.employers))

	slices.SortFunc(employers, func(a, b *Employer) int {
		return strings.Compare(joinKey(a.Source), joinKey(b.Source))
	})

	return employers
}

// Coverage reports how many of the given sources have a row, which is the
// number to lead with whenever this data is presented.
//
// Coverage will be low for a long time: most companies this project crawls are
// private startups on Greenhouse, Ashby and Lever, and SEC EDGAR by definition
// knows nothing about them. Printing the fraction next to the answer is what
// keeps "no data" readable as coverage rather than as breakage.
func (t *Table) Coverage(sources []internal.PostingSource) (matched, total int) {
	for _, source := range sources {
		if source.IsZero() {
			continue
		}

		total++

		if _, ok := t.For(source); ok {
			matched++
		}
	}

	return matched, total
}

// joinKey is the map key for a source identity.
//
// Case is folded on both sides because the table is hand-editable and a
// reviewer retyping "PaloAltoNetworks2" as "paloaltonetworks2" must not silently
// detach the row from its source. That matches how services.Companies already
// deduplicates and how services.SourcesMatching already matches, so the folding
// introduces no collision this project does not already accept.
func joinKey(source internal.PostingSource) string {
	return strings.ToLower(source.Platform) + "\x00" + strings.ToLower(source.Key)
}
