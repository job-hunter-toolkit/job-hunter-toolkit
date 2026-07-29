package corpus

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// Row is one posting's whole life.
//
// It changes only when the board changes something observable, or when the
// posting closes. That is a sizing decision as much as a modelling one: a row
// whose bytes are stable between generations is a row a client's cache, a range
// request and a delta can all skip.
type Row struct {
	// ID is the corpus identity from [Identify]: 32 hex characters, scoped to the
	// integration that published the posting.
	ID string

	// Basis records which rung of the ladder produced ID, so a consumer can
	// distrust a descriptor identity without having to guess which rows have one.
	Basis Basis

	// DedupeKey is byte-identical to shard.PostingKey. The corpus's headline count
	// is the number of distinct dedupe keys among open rows, which is exactly the
	// global union `shard merge` computes today.
	DedupeKey string

	// FirstSeen is the RunAt of the run that first observed this posting. Written
	// once and never rewritten: it is what makes "when did this role appear"
	// answerable, and rewriting it would reset the only dates the corpus exists to
	// hold.
	FirstSeen time.Time

	// LastSeen is stored ONLY when it differs from what [Corpus.LastSeen] would
	// derive — that is, when the posting was last observed by a run that was not
	// allowed to close anything, or at the moment it first goes missing. For an
	// open row in the ordinary case it is the zero time and derived from the
	// source's LastQualifying, so a posting that is simply still open produces
	// byte-identical output every generation.
	//
	// If every row carried an absolute last_seen, every row would change every
	// run, every file's bytes would change every generation, and no client could
	// reuse a cached byte. This is the single most important sizing decision in
	// the format.
	LastSeen time.Time

	// Missing counts qualifying runs of this row's own source that failed to see
	// it. Any observation resets it to zero.
	Missing int

	// Closed is set once the evidence ends the row's life. Nil for a live row.
	Closed *Closure

	// Reopens counts the times a closed row was observed again. A board that
	// re-publishes a filled role, or a close that was wrong, both show up here
	// rather than as a new row with a fresh FirstSeen.
	Reopens int

	// Posting is the most recent observation. It carries Source, so integration
	// identity is not duplicated on the row.
	Posting jobposting.JobPosting
}

// Closure records an interval, not an instant.
//
// Nobody watches a board close a posting. All that is ever known is that it was
// there at LastSeen and gone by ConfirmedAt, and "what closed this week" is a
// query over that interval rather than a lookup of a date the corpus made up.
type Closure struct {
	// LastSeen is the last qualifying observation of the posting.
	LastSeen time.Time

	// ConfirmedAt is the run that reached MissingRuns, or — for the lapsed and
	// retired reasons — the run that noticed the source had stopped being
	// crawled. For those two it is emphatically not a closing date.
	ConfirmedAt time.Time

	// Reason is [ReasonAbsent], [ReasonLapsed] or [ReasonRetired].
	Reason string
}

// Source returns the integration that published the row's posting.
func (r Row) Source() jobposting.PostingSource { return r.Posting.Source }

// sortRows puts rows into the corpus's one total order.
//
// (platform, source key, id). Platform first because it is the most common
// filter and the cheapest dictionary; source key second because grouping a
// source's rows together is what puts its shared URL prefix, company name and
// location vocabulary inside the compressor's window — measured at a 31% saving
// on 780,489 rows for choosing a sort key. The id last makes the order total, so
// two runs over the same postings emit the same bytes.
func sortRows(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		if c := cmp.Compare(a.Posting.Source.Platform, b.Posting.Source.Platform); c != 0 {
			return c
		}

		if c := cmp.Compare(a.Posting.Source.Key, b.Posting.Source.Key); c != 0 {
			return c
		}

		return cmp.Compare(a.ID, b.ID)
	})
}

// Column names. They are the format's schema: a reader addresses a column by
// name, skips one it does not know, and reads one the file lacks as the zero
// value. Adding a field here is therefore not a migration, which is why
// FormatVersion has not moved for one.
const (
	colID          = "id"
	colBasis       = "basis"
	colDedupeKey   = "dedupe_key"
	colFirstSeen   = "first_seen"
	colLastSeen    = "last_seen"
	colMissing     = "missing"
	colReopens     = "reopens"
	colClosedSeen  = "closed_last_seen"
	colClosedAt    = "closed_confirmed_at"
	colClosedWhy   = "closed_reason"
	colPlatform    = "platform"
	colSourceKey   = "source_key"
	colCompany     = "company"
	colURL         = "url"
	colTitle       = "title"
	colLocation    = "location"
	colDepartment  = "department"
	colTeam        = "team"
	colEmployment  = "employment_type"
	colWorkplace   = "workplace_type"
	colSeniority   = "seniority"
	colRequisition = "requisition_id"
	colExternalID  = "external_id"
	colRemote      = "remote"
	colPostedAt    = "posted_at"
	colUpdatedAt   = "updated_at"
	colComp        = "compensation"
	colCompMin     = "compensation_min"
	colCompMax     = "compensation_max"
	colCompCcy     = "compensation_currency"
	colCompPeriod  = "compensation_period"
	colCompSummary = "compensation_summary"
	colCompProv    = "compensation_provenance"
)

// Tri-state encoding for *bool. The zero value has to mean "the board published
// no structured field", which is the common case, so nil cannot share a slot
// with false.
const (
	remoteUnset = 0
	remoteFalse = 1
	remoteTrue  = 2
)

// Timestamps are stored as Unix milliseconds in UTC, with 0 reserved for the
// zero time.
//
// Milliseconds rather than seconds because Lever publishes epoch milliseconds
// and truncating would make the corpus lossy about data a board actually
// published; storage-engine.md §9 notes timestamps are 44% of the file and
// seconds would shrink them, which is a real trade this deliberately declines.
// The reserved zero costs the ability to represent 1970-01-01T00:00:00.000Z,
// which is not a job posting date.
func encodeTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UTC().UnixMilli()
}

func decodeTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}

	return time.UnixMilli(ms).UTC()
}

// encodeFloat renders a compensation figure so it round-trips exactly.
//
// Strings rather than a fourth encoding: 'g' with precision -1 is the shortest
// representation that parses back to the identical float64, the columns are
// empty on the large majority of postings because most boards publish no
// structured pay field at all, and an empty string costs one byte before
// compression. A float column encoding would be more bytes of format for less
// than a percent of the file.
func encodeFloat(f float64) string {
	if f == 0 {
		return ""
	}

	return strconv.FormatFloat(f, 'g', -1, 64)
}

func decodeFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}

	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("not a finite number")
	}

	return f, nil
}

// buildTable encodes rows into a .jhtc, column by column.
//
// It allocates one slice per column and releases it as soon as the column is
// encoded, so peak memory is the rows plus one column rather than the rows plus
// the whole table. That still holds every row in memory, which is the format's
// stated model — the corpus is rewritten whole every run — and it is the number
// the benchmark in this package reports rather than assumes.
func buildTable(rows []Row) (*Builder, error) {
	builder := NewBuilder(len(rows))

	strs := func(name string, get func(Row) string) error {
		values := make([]string, len(rows))
		for i := range rows {
			values[i] = get(rows[i])
		}

		return builder.AddStrings(name, values)
	}

	ints := func(name string, get func(Row) int64) error {
		values := make([]int64, len(rows))
		for i := range rows {
			values[i] = get(rows[i])
		}

		return builder.AddInts(name, values)
	}

	// Footer order is this order, and [Builder.ContentDigest] hashes in footer
	// order, so this list is part of the format's identity. Row identity first,
	// lifecycle second, the posting third: a client that only wants to count open
	// rows fetches the first six columns and stops.
	steps := []func() error{
		func() error { return strs(colID, func(r Row) string { return r.ID }) },
		func() error { return strs(colBasis, func(r Row) string { return string(r.Basis) }) },
		func() error { return strs(colDedupeKey, func(r Row) string { return r.DedupeKey }) },
		func() error { return ints(colFirstSeen, func(r Row) int64 { return encodeTime(r.FirstSeen) }) },
		func() error { return ints(colLastSeen, func(r Row) int64 { return encodeTime(r.LastSeen) }) },
		func() error { return ints(colMissing, func(r Row) int64 { return int64(r.Missing) }) },
		func() error { return ints(colReopens, func(r Row) int64 { return int64(r.Reopens) }) },
		func() error {
			return ints(colClosedSeen, func(r Row) int64 {
				if r.Closed == nil {
					return 0
				}

				return encodeTime(r.Closed.LastSeen)
			})
		},
		func() error {
			return ints(colClosedAt, func(r Row) int64 {
				if r.Closed == nil {
					return 0
				}

				return encodeTime(r.Closed.ConfirmedAt)
			})
		},
		// closed_reason doubles as the presence flag for Closure: the three legal
		// reasons are all non-empty, so an empty string is a live row and no
		// separate column is needed.
		func() error {
			return strs(colClosedWhy, func(r Row) string {
				if r.Closed == nil {
					return ""
				}

				return r.Closed.Reason
			})
		},
		func() error { return strs(colPlatform, func(r Row) string { return r.Posting.Source.Platform }) },
		func() error { return strs(colSourceKey, func(r Row) string { return r.Posting.Source.Key }) },
		func() error { return strs(colCompany, func(r Row) string { return r.Posting.Company }) },
		func() error { return strs(colURL, func(r Row) string { return r.Posting.URL }) },
		func() error { return strs(colTitle, func(r Row) string { return r.Posting.Title }) },
		func() error { return strs(colLocation, func(r Row) string { return r.Posting.Location }) },
		func() error { return strs(colDepartment, func(r Row) string { return r.Posting.Department }) },
		func() error { return strs(colTeam, func(r Row) string { return r.Posting.Team }) },
		func() error {
			return strs(colEmployment, func(r Row) string { return string(r.Posting.EmploymentType) })
		},
		func() error {
			return strs(colWorkplace, func(r Row) string { return string(r.Posting.WorkplaceType) })
		},
		func() error { return strs(colSeniority, func(r Row) string { return r.Posting.Seniority }) },
		func() error { return strs(colRequisition, func(r Row) string { return r.Posting.RequisitionID }) },
		func() error { return strs(colExternalID, func(r Row) string { return r.Posting.ExternalID }) },
		func() error {
			return ints(colRemote, func(r Row) int64 {
				switch {
				case r.Posting.Remote == nil:
					return remoteUnset
				case *r.Posting.Remote:
					return remoteTrue
				default:
					return remoteFalse
				}
			})
		},
		func() error { return ints(colPostedAt, func(r Row) int64 { return encodeTime(r.Posting.PostedAt) }) },
		func() error { return ints(colUpdatedAt, func(r Row) int64 { return encodeTime(r.Posting.UpdatedAt) }) },
		// An explicit presence column, because a non-nil Compensation whose every
		// field is empty is a different fact from a nil one: it means the board had
		// a pay field and left it blank. A constant column costs a handful of bytes.
		func() error {
			return ints(colComp, func(r Row) int64 {
				if r.Posting.Compensation == nil {
					return 0
				}

				return 1
			})
		},
		func() error { return strs(colCompMin, func(r Row) string { return compField(r, compMin) }) },
		func() error { return strs(colCompMax, func(r Row) string { return compField(r, compMax) }) },
		func() error { return strs(colCompCcy, func(r Row) string { return compField(r, compCurrency) }) },
		func() error { return strs(colCompPeriod, func(r Row) string { return compField(r, compPeriod) }) },
		func() error { return strs(colCompSummary, func(r Row) string { return compField(r, compSummary) }) },
		func() error { return strs(colCompProv, func(r Row) string { return compField(r, compProvenance) }) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}

	return builder, nil
}

type compAccessor uint8

const (
	compMin compAccessor = iota
	compMax
	compCurrency
	compPeriod
	compSummary
	compProvenance
)

func compField(r Row, which compAccessor) string {
	c := r.Posting.Compensation
	if c == nil {
		return ""
	}

	switch which {
	case compMin:
		return encodeFloat(c.Min)
	case compMax:
		return encodeFloat(c.Max)
	case compCurrency:
		return c.Currency
	case compPeriod:
		return string(c.Period)
	case compSummary:
		return c.Summary
	case compProvenance:
		return string(c.Provenance)
	default:
		return ""
	}
}

// readRows materializes every row of a table.
//
// This is the resident read mode: decode the columns once, then answer from
// memory. A caller that needs one aggregate over two columns should use
// [Table.Strings] and [Table.Ints] directly and never build a Row at all, which
// is the difference storage-engine.md §4 measures as 56 ms against a 325 ms
// load.
func readRows(table *Table) ([]Row, error) {
	rows := make([]Row, table.Rows())

	type stringColumn struct {
		name string
		set  func(*Row, string)
	}

	type intColumn struct {
		name string
		set  func(*Row, int64)
	}

	strings := []stringColumn{
		{colID, func(r *Row, v string) { r.ID = v }},
		{colBasis, func(r *Row, v string) { r.Basis = Basis(v) }},
		{colDedupeKey, func(r *Row, v string) { r.DedupeKey = v }},
		{colClosedWhy, func(r *Row, v string) {
			if v == "" {
				return
			}

			if r.Closed == nil {
				r.Closed = &Closure{}
			}

			r.Closed.Reason = v
		}},
		{colPlatform, func(r *Row, v string) { r.Posting.Source.Platform = v }},
		{colSourceKey, func(r *Row, v string) { r.Posting.Source.Key = v }},
		{colCompany, func(r *Row, v string) { r.Posting.Company = v }},
		{colURL, func(r *Row, v string) { r.Posting.URL = v }},
		{colTitle, func(r *Row, v string) { r.Posting.Title = v }},
		{colLocation, func(r *Row, v string) { r.Posting.Location = v }},
		{colDepartment, func(r *Row, v string) { r.Posting.Department = v }},
		{colTeam, func(r *Row, v string) { r.Posting.Team = v }},
		{colEmployment, func(r *Row, v string) { r.Posting.EmploymentType = jobposting.EmploymentType(v) }},
		{colWorkplace, func(r *Row, v string) { r.Posting.WorkplaceType = jobposting.WorkplaceType(v) }},
		{colSeniority, func(r *Row, v string) { r.Posting.Seniority = v }},
		{colRequisition, func(r *Row, v string) { r.Posting.RequisitionID = v }},
		{colExternalID, func(r *Row, v string) { r.Posting.ExternalID = v }},
	}

	ints := []intColumn{
		{colFirstSeen, func(r *Row, v int64) { r.FirstSeen = decodeTime(v) }},
		{colLastSeen, func(r *Row, v int64) { r.LastSeen = decodeTime(v) }},
		{colMissing, func(r *Row, v int64) { r.Missing = int(v) }},
		{colReopens, func(r *Row, v int64) { r.Reopens = int(v) }},
		{colPostedAt, func(r *Row, v int64) { r.Posting.PostedAt = decodeTime(v) }},
		{colUpdatedAt, func(r *Row, v int64) { r.Posting.UpdatedAt = decodeTime(v) }},
		{colRemote, func(r *Row, v int64) {
			switch v {
			case remoteTrue:
				yes := true
				r.Posting.Remote = &yes
			case remoteFalse:
				no := false
				r.Posting.Remote = &no
			}
		}},
	}

	// closed_reason is applied before the two closed timestamps so the Closure
	// exists to receive them; a closed timestamp with no reason is a corrupt file
	// and is reported rather than silently resurrecting the row.
	for _, column := range strings {
		values, err := table.Strings(column.name)
		if err != nil {
			return nil, err
		}

		for i := range rows {
			column.set(&rows[i], values[i])
		}
	}

	for _, column := range ints {
		values, err := table.Ints(column.name)
		if err != nil {
			return nil, err
		}

		for i := range rows {
			column.set(&rows[i], values[i])
		}
	}

	if err := readClosure(table, rows); err != nil {
		return nil, err
	}

	return rows, readCompensation(table, rows)
}

func readClosure(table *Table, rows []Row) error {
	seen, err := table.Ints(colClosedSeen)
	if err != nil {
		return err
	}

	at, err := table.Ints(colClosedAt)
	if err != nil {
		return err
	}

	for i := range rows {
		if rows[i].Closed == nil {
			if seen[i] != 0 || at[i] != 0 {
				return formatErr("row %d has closure timestamps but no closure reason", i)
			}

			continue
		}

		rows[i].Closed.LastSeen = decodeTime(seen[i])
		rows[i].Closed.ConfirmedAt = decodeTime(at[i])
	}

	return nil
}

func readCompensation(table *Table, rows []Row) error {
	present, err := table.Ints(colComp)
	if err != nil {
		return err
	}

	columns := map[string][]string{}
	for _, name := range []string{colCompMin, colCompMax, colCompCcy, colCompPeriod, colCompSummary, colCompProv} {
		if columns[name], err = table.Strings(name); err != nil {
			return err
		}
	}

	for i := range rows {
		if present[i] == 0 {
			continue
		}

		minimum, err := decodeFloat(columns[colCompMin][i])
		if err != nil {
			return formatErr("row %d compensation_min: %v", i, err)
		}

		maximum, err := decodeFloat(columns[colCompMax][i])
		if err != nil {
			return formatErr("row %d compensation_max: %v", i, err)
		}

		rows[i].Posting.Compensation = &jobposting.Compensation{
			Min:        minimum,
			Max:        maximum,
			Currency:   columns[colCompCcy][i],
			Period:     jobposting.Period(columns[colCompPeriod][i]),
			Summary:    columns[colCompSummary][i],
			Provenance: jobposting.Provenance(columns[colCompProv][i]),
		}
	}

	return nil
}
