package enrich

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// The committed tables are tab-separated text with a named header row, and that
// choice is load-bearing rather than lazy.
//
// The whole design rests on a human being able to read a diff and say "no, that
// CIK belongs to a different company". A gzipped or binary table would make the
// review step, which is the only thing standing between this feature and
// silently wrong data, impossible to perform in a pull request. Tab separation
// rather than CSV because none of the values here legitimately contain tabs, so
// there is no quoting to get wrong, and a stray quote in a company name cannot
// shift every later column.
//
// Size is not a reason to compress yet: the crawl covers ~2,131 sources, so even
// a fully populated employer table is a few hundred kilobytes of text. The wage
// tables in docs are the ones that will eventually need compression, and they
// can adopt it without changing this reader, because the reader takes an
// io.Reader.

// tsvComment marks a line the reader ignores.
//
// The tables are reviewed by hand, and a reviewer who cannot leave a note next
// to a row explaining why a match was accepted will leave the note nowhere.
const tsvComment = '#'

// tsvTable is one parsed table: the header, and the rows keyed by column name.
type tsvTable struct {
	// name identifies the file in error messages. A parse failure that does not
	// say which of three embedded tables failed is a scavenger hunt.
	name string

	columns []string
	rows    []tsvRow
}

// tsvRow is one record, addressable by column name and aware of which line it
// came from so an error can point at it.
type tsvRow struct {
	table  string
	line   int
	values map[string]string
}

// readTSV parses a named-header tab-separated table, requiring exactly the given
// columns.
//
// Both directions are checked. A missing column is obvious; an unexpected one is
// the interesting case, because the way this file gets corrupted in practice is
// a typo in a generator's header or a half-finished schema change, and silently
// ignoring a column named "employes" would ship an empty headcount for every
// company with no error anywhere.
func readTSV(name string, r io.Reader, want []string) (*tsvTable, error) {
	scanner := bufio.NewScanner(r)

	// Rows are short, but a corrupted file can present as one enormous line, and
	// the default 64 KiB limit would report that as a truncated table rather
	// than as the corruption it is.
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)

	table := &tsvTable{name: name}

	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSuffix(scanner.Text(), "\r")

		if line == 1 {
			// A UTF-8 BOM in front of the first column name would make it
			// unmatchable, and spreadsheet exports add one. This table is
			// expected to be edited by hand, sometimes with a spreadsheet.
			text = strings.TrimPrefix(text, "\ufeff")
		}

		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, string(tsvComment)) {
			continue
		}

		fields := strings.Split(text, "\t")

		if table.columns == nil {
			for i, column := range fields {
				fields[i] = strings.TrimSpace(column)
			}

			if err := checkColumns(name, fields, want); err != nil {
				return nil, err
			}

			table.columns = fields

			continue
		}

		if len(fields) != len(table.columns) {
			return nil, fmt.Errorf("reading %s line %d: got %d tab-separated fields, want %d (%s)",
				name, line, len(fields), len(table.columns), strings.Join(table.columns, ", "))
		}

		values := make(map[string]string, len(fields))
		for i, column := range table.columns {
			values[column] = strings.TrimSpace(fields[i])
		}

		table.rows = append(table.rows, tsvRow{table: name, line: line, values: values})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	if table.columns == nil {
		return nil, fmt.Errorf("reading %s: no header row; the first non-comment line must name the columns (%s)",
			name, strings.Join(want, ", "))
	}

	return table, nil
}

// checkColumns reports how a header differs from the expected one, naming both
// halves of the difference in one error so a schema change is fixed in one pass.
func checkColumns(name string, got, want []string) error {
	var missing, unexpected []string

	present := make(map[string]bool, len(got))
	for _, column := range got {
		present[column] = true
	}

	expected := make(map[string]bool, len(want))
	for _, column := range want {
		expected[column] = true

		if !present[column] {
			missing = append(missing, column)
		}
	}

	for _, column := range got {
		if !expected[column] {
			unexpected = append(unexpected, column)
		}
	}

	if len(missing) == 0 && len(unexpected) == 0 {
		return nil
	}

	return fmt.Errorf("reading %s: header does not match the schema: missing [%s], unexpected [%s]; expected exactly %s",
		name, strings.Join(missing, " "), strings.Join(unexpected, " "), strings.Join(want, ", "))
}

// str returns a column's value.
func (r tsvRow) str(column string) string { return r.values[column] }

// int returns a column's value as an integer, treating an empty cell as absent.
//
// Empty rather than zero is the only honest spelling of "unknown headcount", and
// the table has to be able to say it, so a blank cell is not an error.
func (r tsvRow) int(column string) (int, error) {
	raw := r.str(column)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s line %d: column %q: %w", r.table, r.line, column, err)
	}

	return value, nil
}

// float returns a column's value as a float, treating an empty cell as absent.
func (r tsvRow) float(column string) (float64, error) {
	raw := r.str(column)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s line %d: column %q: %w", r.table, r.line, column, err)
	}

	return value, nil
}

// boolPtr returns a column's value as a tri-state: nil for an empty cell.
//
// This is the whole reason the table is text with explicit blanks. "public" has
// three answers here — yes, no, and nobody has looked — and the third is the
// most common one this project will ever record.
func (r tsvRow) boolPtr(column string) (*bool, error) {
	raw := r.str(column)
	if raw == "" {
		return nil, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%s line %d: column %q: %w (use true, false, or an empty cell for unknown)",
			r.table, r.line, column, err)
	}

	return &value, nil
}

// list returns a column's value as a space-separated list, which is how the
// table spells the handful of genuinely multi-valued fields without needing a
// quoting rule.
func (r tsvRow) list(column string) []string {
	return strings.Fields(r.str(column))
}

// tsvWriter writes a named-header tab-separated table.
type tsvWriter struct {
	w       *bufio.Writer
	columns []string
	wrote   bool
}

// newTSVWriter returns a writer for the given columns.
func newTSVWriter(w io.Writer, columns []string) *tsvWriter {
	return &tsvWriter{w: bufio.NewWriter(w), columns: columns}
}

// comment writes a note above the header. Generated tables carry their own
// provenance, so a reviewer opening the file learns what produced it and when
// without leaving the file.
func (w *tsvWriter) comment(format string, args ...any) error {
	if w.wrote {
		return fmt.Errorf("writing table: comments must precede the header row")
	}

	_, err := fmt.Fprintf(w.w, "%c %s\n", tsvComment, fmt.Sprintf(format, args...))

	return err
}

// write appends one record, taking values by column name so a caller cannot
// silently transpose two columns by reordering its arguments.
func (w *tsvWriter) write(values map[string]string) error {
	if !w.wrote {
		if _, err := fmt.Fprintln(w.w, strings.Join(w.columns, "\t")); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}

		w.wrote = true
	}

	if len(values) > len(w.columns) {
		return fmt.Errorf("writing row: %d values for %d columns (%s)",
			len(values), len(w.columns), strings.Join(unknownColumns(values, w.columns), ", "))
	}

	fields := make([]string, len(w.columns))

	for i, column := range w.columns {
		value := values[column]

		// A tab or newline inside a value would silently shift every later
		// column of that row, and the row would still parse. Refusing is the
		// only outcome that cannot corrupt a table quietly; nothing in these
		// datasets legitimately contains either character.
		if strings.ContainsAny(value, "\t\r\n") {
			return fmt.Errorf("writing row: column %q value %q contains a tab or newline, which would shift every later column", column, value)
		}

		fields[i] = value
	}

	if _, err := fmt.Fprintln(w.w, strings.Join(fields, "\t")); err != nil {
		return fmt.Errorf("writing row: %w", err)
	}

	return nil
}

// flush writes the header even when no rows were written, so a table with no
// matches is still a valid, self-describing, parseable file rather than an
// empty one.
//
// That case is not hypothetical: the first committed table has no rows at all,
// because no generator run against the live sources has happened yet.
func (w *tsvWriter) flush() error {
	if !w.wrote {
		if _, err := fmt.Fprintln(w.w, strings.Join(w.columns, "\t")); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}

		w.wrote = true
	}

	return w.w.Flush()
}

// unknownColumns names the value keys a writer has no column for, for the error
// message above.
func unknownColumns(values map[string]string, columns []string) []string {
	known := make(map[string]bool, len(columns))
	for _, column := range columns {
		known[column] = true
	}

	var unknown []string

	for column := range values {
		if !known[column] {
			unknown = append(unknown, column)
		}
	}

	slices.Sort(unknown)

	return unknown
}
