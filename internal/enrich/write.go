package enrich

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// EmployerColumns returns the employer table's schema, in file order.
//
// Exported so the generator writes exactly what the loader reads. The two used
// to be able to drift, and a generator that emitted a column the loader did not
// expect would produce a table that parses nowhere — discovered, at best, in
// review, and at worst in a release.
func EmployerColumns() []string { return slices.Clone(employerColumns) }

// WageColumns returns the wage benchmark table's schema, in file order.
func WageColumns() []string { return slices.Clone(wageColumns) }

// WriteEmployers writes an employer table: the given notes as comments, then the
// header, then one row per employer sorted by platform and key.
//
// Serialization lives here, next to the parser, so a round trip is guaranteed by
// construction rather than by two files agreeing. TestEmployerTableRoundTrips
// writes a table with this and reads it back with [LoadFS].
//
// Sorted output is not cosmetic. The generator rewrites this file on every run
// and the result is reviewed as a diff; map-ordered output would show every row
// as changed on every refresh, and a reviewer who has to read 2,000 spurious
// changes will not find the one real one.
func WriteEmployers(w io.Writer, employers []*Employer, notes ...string) error {
	writer := newTSVWriter(w, employerColumns)

	for _, note := range notes {
		for _, line := range strings.Split(note, "\n") {
			if err := writer.comment("%s", line); err != nil {
				return fmt.Errorf("writing employer table notes: %w", err)
			}
		}
	}

	sorted := slices.Clone(employers)
	slices.SortFunc(sorted, func(a, b *Employer) int {
		return strings.Compare(joinKey(a.Source), joinKey(b.Source))
	})

	for _, employer := range sorted {
		if employer == nil {
			continue
		}

		if err := writer.write(employerRow(employer)); err != nil {
			return fmt.Errorf("writing employer %s/%s: %w", employer.Source.Platform, employer.Source.Key, err)
		}
	}

	if err := writer.flush(); err != nil {
		return fmt.Errorf("writing employer table: %w", err)
	}

	return nil
}

// employerRow renders one employer as table cells.
//
// Unknown values are written as empty cells rather than as zeros or
// placeholders, matching what the CSV writer in package main already does for
// postings: an empty cell is read as absent by every spreadsheet and every
// parser, whereas "0" is a headcount somebody measured.
func employerRow(employer *Employer) map[string]string {
	row := map[string]string{
		"platform":         employer.Source.Platform,
		"key":              employer.Source.Key,
		"company":          employer.Company,
		"legal_name":       employer.LegalName,
		"cik":              employer.CIK,
		"ticker":           employer.Ticker,
		"exchange":         employer.Exchange,
		"sic":              employer.SIC,
		"industry":         employer.Industry,
		"headquarters":     employer.Headquarters,
		"parent":           employer.Parent,
		"wikidata_id":      employer.WikidataID,
		"match_method":     string(employer.Match.Method),
		"match_confidence": string(employer.Match.Confidence),
		"data_sources":     strings.Join(employer.Match.DataSources, " "),
		"retrieved":        employer.Match.RetrievedAt,
	}

	if employer.Public != nil {
		row["public"] = strconv.FormatBool(*employer.Public)
	}

	if employer.Employees > 0 {
		row["employees"] = strconv.Itoa(employer.Employees)
	}

	if employer.Founded > 0 {
		row["founded"] = strconv.Itoa(employer.Founded)
	}

	return row
}
