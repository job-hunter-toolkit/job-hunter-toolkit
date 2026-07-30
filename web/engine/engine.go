// Package engine is the query core of the corpus-backed web surface.
//
// It is the Go that the browser runs: web/wasm compiles it to WebAssembly and
// exposes it to a thin DOM layer, and this package deliberately contains no
// syscall/js so that every line of it is testable with an ordinary `go test`
// on the host. The wasm bridge is a translation layer and nothing more.
//
// The engine speaks the same query vocabulary as the CLI — package query is
// the filter, package corpus is the store — so the website cannot drift into a
// second, slightly different definition of "remote" or "posted since". The
// requests and responses cross the JS boundary as JSON, which keeps the bridge
// to a handful of string copies.
//
// # Honesty
//
// [Summary] carries the generation's RunAt, Partial flag and state counts
// precisely so the UI can put "this is a snapshot from <date>, <complete or
// partial>" in front of every result. A search response never mixes states
// silently: closed and lapsed rows are excluded unless the request asks for
// them, and every returned item names its state.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/query"
)

// Engine answers queries against one opened corpus generation.
//
// The read model is resident: [Engine.Load] materializes every row once, and
// searches after that are in-memory scans. docs/design/storage-engine.md §6
// measured that trade for the browser, and a browser tab is the many-queries
// case.
type Engine struct {
	corpus *corpus.Corpus

	// now is the single clock reading lifecycle states are computed against.
	// It is taken once in [Open] so that paging through results cannot watch a
	// posting drift from open to stale between two clicks.
	now time.Time

	rows []record

	// order is row indexes in presentation order, computed once at load:
	// PostedAt descending with undated rows last, then FirstSeen descending,
	// then company and title ascending, then table row index so the order is
	// total and a reload shows the same page.
	order []int
}

// record is one row as the web surface needs it, and nothing more.
//
// It is deliberately not corpus.Row. A full Row carries the corpus's identity
// and lifecycle bookkeeping — id, basis, dedupe_key, closure timestamps,
// missing counts — which no query predicate reads and no result card shows.
// At corpus volume those columns are real money: measured at 800,000 rows
// under Node, loading full rows cost 12.2 s, 27 MiB of column fetches and
// 1.39 GiB of wasm linear memory; the id and dedupe_key columns alone are the
// two least compressible in the file. This projection is what [Engine.Load]
// pays for instead, and the same measurement after the change is the number
// web/README.md quotes.
type record struct {
	posting   jobposting.JobPosting
	firstSeen time.Time
	state     corpus.State
}

// Summary is what the UI must show before it shows a single posting: which
// generation this is, when the data was crawled, and whether the producing
// crawl finished.
type Summary struct {
	Generation      int64   `json:"generation"`
	RunAt           string  `json:"run_at,omitempty"`
	AgeHours        float64 `json:"age_hours"`
	Partial         bool    `json:"partial"`
	Writer          string  `json:"writer,omitempty"`
	Rows            int     `json:"rows"`
	Sources         int     `json:"sources"`
	Open            int     `json:"open"`
	Stale           int     `json:"stale"`
	Closed          int     `json:"closed"`
	Lapsed          int     `json:"lapsed"`
	ContentDigest   string  `json:"content_digest,omitempty"`
	FormatVersion   int     `json:"format_version"`
	IdentityVersion int     `json:"identity_version"`
}

// SearchRequest is the query vocabulary as it crosses the JS boundary. The
// fields mirror query.Query one for one; the only additions are paging and the
// closed-rows switch, which are presentation concerns the shared vocabulary
// deliberately does not carry.
type SearchRequest struct {
	Titles          []string `json:"titles,omitempty"`
	ExcludeTitles   []string `json:"exclude_titles,omitempty"`
	Locations       []string `json:"locations,omitempty"`
	Companies       []string `json:"companies,omitempty"`
	Departments     []string `json:"departments,omitempty"`
	Remote          bool     `json:"remote,omitempty"`
	HasCompensation bool     `json:"has_compensation,omitempty"`
	MinAnnual       float64  `json:"min_annual,omitempty"`
	EmploymentTypes []string `json:"employment_types,omitempty"`
	WorkplaceTypes  []string `json:"workplace_types,omitempty"`

	// PostedSinceDays bounds PostedAt to the last N days, measured from the
	// engine's single clock reading. Zero means no bound.
	PostedSinceDays int `json:"posted_since_days,omitempty"`

	// IncludeClosed widens the search beyond rows currently believed open
	// (states open and stale) to the corpus's whole history, closed and lapsed
	// rows included. Off by default: a job board that quietly listed filled
	// roles would be lying.
	IncludeClosed bool `json:"include_closed,omitempty"`

	// Offset and Limit page the matched set in presentation order. Limit is
	// clamped to [1, MaxLimit]; zero means MaxLimit.
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`
}

// MaxLimit bounds one page of results. The matched set can be six figures and
// the DOM cannot be, so the response is always a bounded window plus an honest
// total.
const MaxLimit = 100

// SearchResponse is one page of matches plus the totals the UI needs to say
// what the page is a window into.
type SearchResponse struct {
	// Matched counts every row satisfying the query, not just the page.
	Matched int `json:"matched"`

	// States counts the matched rows by lifecycle state, so "1,204 matches"
	// can honestly read "1,180 open, 24 stale".
	States map[string]int `json:"states"`

	Offset int    `json:"offset"`
	Items  []Item `json:"items"`
}

// Item is one posting as the results list renders it.
type Item struct {
	Title          string `json:"title"`
	Company        string `json:"company"`
	Location       string `json:"location,omitempty"`
	URL            string `json:"url,omitempty"`
	Platform       string `json:"platform,omitempty"`
	Department     string `json:"department,omitempty"`
	Team           string `json:"team,omitempty"`
	EmploymentType string `json:"employment_type,omitempty"`
	WorkplaceType  string `json:"workplace_type,omitempty"`
	Seniority      string `json:"seniority,omitempty"`
	Remote         bool   `json:"remote,omitempty"`
	Compensation   string `json:"compensation,omitempty"`
	PostedAt       string `json:"posted_at,omitempty"`
	FirstSeen      string `json:"first_seen,omitempty"`
	State          string `json:"state"`
}

// Open reads a corpus's manifest, source states and table footer through the
// store — a few kilobytes — and no column data. The caller decides when to pay
// for [Engine.Load].
func Open(ctx context.Context, store corpus.Store, now time.Time) (*Engine, error) {
	c, err := corpus.Open(ctx, store)
	if err != nil {
		return nil, err
	}

	return &Engine{corpus: c, now: now.UTC()}, nil
}

// Summary describes the opened generation. It is available before Load.
func (e *Engine) Summary() Summary {
	m := e.corpus.Manifest()

	s := Summary{
		Generation:      m.Generation,
		Partial:         m.Partial,
		Writer:          m.Writer,
		Rows:            m.Rows,
		Sources:         m.Sources,
		Open:            m.Open,
		Stale:           m.Stale,
		Closed:          m.Closed,
		Lapsed:          m.Lapsed,
		ContentDigest:   m.ContentDigest,
		FormatVersion:   m.FormatVersion,
		IdentityVersion: m.IdentityVersion,
	}

	if !m.RunAt.IsZero() {
		s.RunAt = m.RunAt.UTC().Format(time.RFC3339)
		s.AgeHours = e.now.Sub(m.RunAt).Hours()
	}

	return s
}

// The column names this surface reads, mirroring the .jhtc schema that
// internal/corpus writes (see internal/corpus/row.go). The format's contract
// is that readers address columns by name and read an absent column as zero
// values — which for the required core below would silently turn every search
// into "nothing matches", so those are checked with Table.Has and refused
// loudly instead.
const (
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
	colClosedWhy   = "closed_reason"
	colFirstSeen   = "first_seen"
	colPostedAt    = "posted_at"
	colRemote      = "remote"
	colComp        = "compensation"
	colCompMin     = "compensation_min"
	colCompMax     = "compensation_max"
	colCompCcy     = "compensation_currency"
	colCompPeriod  = "compensation_period"
	colCompSummary = "compensation_summary"
)

// requiredColumns is the core a corpus must carry for this surface to answer
// anything truthfully.
var requiredColumns = []string{
	colPlatform, colSourceKey, colCompany, colURL, colTitle, colLocation, colFirstSeen,
}

// Load materializes the columns the queries and the result cards need — and
// only those — then computes each row's lifecycle state against the engine's
// clock reading. In a range-fetching store each column is one contiguous
// request, so the columns this deliberately skips (row identity, dedupe keys,
// closure timestamps, the audit fields) are bytes that never leave the host.
func (e *Engine) Load(_ context.Context) error {
	if e.rows != nil {
		return nil
	}

	table := e.corpus.Table()

	for _, name := range requiredColumns {
		if !table.Has(name) {
			return fmt.Errorf("engine: corpus has no %q column; refusing to search a corpus this build cannot read truthfully", name)
		}
	}

	rows := make([]record, table.Rows())

	// String columns, decoded one at a time so peak memory is the records
	// plus one column, never the records plus every column.
	for _, column := range []struct {
		name string
		set  func(*record, string)
	}{
		{colPlatform, func(r *record, v string) { r.posting.Source.Platform = v }},
		{colSourceKey, func(r *record, v string) { r.posting.Source.Key = v }},
		{colCompany, func(r *record, v string) { r.posting.Company = v }},
		{colURL, func(r *record, v string) { r.posting.URL = v }},
		{colTitle, func(r *record, v string) { r.posting.Title = v }},
		{colLocation, func(r *record, v string) { r.posting.Location = v }},
		{colDepartment, func(r *record, v string) { r.posting.Department = v }},
		{colTeam, func(r *record, v string) { r.posting.Team = v }},
		{colEmployment, func(r *record, v string) { r.posting.EmploymentType = jobposting.EmploymentType(v) }},
		{colWorkplace, func(r *record, v string) { r.posting.WorkplaceType = jobposting.WorkplaceType(v) }},
		{colSeniority, func(r *record, v string) { r.posting.Seniority = v }},
	} {
		values, err := table.Strings(column.name)
		if err != nil {
			return err
		}

		for i := range rows {
			column.set(&rows[i], values[i])
		}
	}

	// Timestamps are Unix milliseconds with 0 reserved for "the board did not
	// say" — the .jhtc encoding internal/corpus/row.go documents.
	firstSeen, err := table.Ints(colFirstSeen)
	if err != nil {
		return err
	}

	postedAt, err := table.Ints(colPostedAt)
	if err != nil {
		return err
	}

	remote, err := table.Ints(colRemote)
	if err != nil {
		return err
	}

	for i := range rows {
		rows[i].firstSeen = decodeTime(firstSeen[i])
		rows[i].posting.PostedAt = decodeTime(postedAt[i])

		// The tri-state remote column: 0 unset, 1 false, 2 true. The two
		// pointer values are shared so 800k rows cost two allocations.
		switch remote[i] {
		case remoteFalse:
			rows[i].posting.Remote = &remoteNo
		case remoteTrue:
			rows[i].posting.Remote = &remoteYes
		}
	}

	if err := e.loadCompensation(table, rows); err != nil {
		return err
	}

	if err := e.loadStates(table, rows); err != nil {
		return err
	}

	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(x, y int) bool {
		a, b := &rows[order[x]], &rows[order[y]]

		// Dated postings first, newest first. An undated posting is not old,
		// it is undated, but a list has to put it somewhere and burying it
		// beats letting it pretend to be today's.
		if !a.posting.PostedAt.Equal(b.posting.PostedAt) {
			if a.posting.PostedAt.IsZero() || b.posting.PostedAt.IsZero() {
				return b.posting.PostedAt.IsZero()
			}

			return a.posting.PostedAt.After(b.posting.PostedAt)
		}

		if !a.firstSeen.Equal(b.firstSeen) {
			return a.firstSeen.After(b.firstSeen)
		}

		if a.posting.Company != b.posting.Company {
			return a.posting.Company < b.posting.Company
		}

		if a.posting.Title != b.posting.Title {
			return a.posting.Title < b.posting.Title
		}

		// The table's row order is itself deterministic, so the index is a
		// stable final tiebreak.
		return order[x] < order[y]
	})

	e.rows, e.order = rows, order

	return nil
}

// Shared pointees for the tri-state remote column.
var (
	remoteYes = true
	remoteNo  = false
)

// The tri-state encoding for the remote column, as row.go defines it.
const (
	remoteFalse = 1
	remoteTrue  = 2
)

// decodeTime reverses the corpus's timestamp encoding: Unix milliseconds in
// UTC, 0 reserved for the zero time.
func decodeTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}

	return time.UnixMilli(ms).UTC()
}

// loadCompensation attaches published pay ranges. The presence column keeps
// "the board had a pay field and left it blank" distinct from "no pay field",
// and only present rows allocate.
func (e *Engine) loadCompensation(table *corpus.Table, rows []record) error {
	present, err := table.Ints(colComp)
	if err != nil {
		return err
	}

	columns := map[string][]string{}
	for _, name := range []string{colCompMin, colCompMax, colCompCcy, colCompPeriod, colCompSummary} {
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
			return fmt.Errorf("engine: row %d compensation_min: %w", i, err)
		}

		maximum, err := decodeFloat(columns[colCompMax][i])
		if err != nil {
			return fmt.Errorf("engine: row %d compensation_max: %w", i, err)
		}

		rows[i].posting.Compensation = &jobposting.Compensation{
			Min:      minimum,
			Max:      maximum,
			Currency: columns[colCompCcy][i],
			Period:   jobposting.Period(columns[colCompPeriod][i]),
			Summary:  columns[colCompSummary][i],
		}
	}

	return nil
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
		return 0, fmt.Errorf("not a finite number: %q", s)
	}

	return f, nil
}

// loadStates computes each row's lifecycle state through corpus.State — the
// same derivation every other surface uses — feeding it the two inputs that
// function actually reads: the closure reason and the row's source.
func (e *Engine) loadStates(table *corpus.Table, rows []record) error {
	reasons, err := table.Strings(colClosedWhy)
	if err != nil {
		return err
	}

	for i := range rows {
		synthetic := corpus.Row{Posting: jobposting.JobPosting{Source: rows[i].posting.Source}}
		if reasons[i] != "" {
			synthetic.Closed = &corpus.Closure{Reason: reasons[i]}
		}

		rows[i].state = e.corpus.State(synthetic, e.now)
	}

	return nil
}

// Loaded reports whether Load has run.
func (e *Engine) Loaded() bool { return e.rows != nil }

// Search evaluates a request against the loaded rows.
func (e *Engine) Search(req SearchRequest) (SearchResponse, error) {
	if e.rows == nil {
		return SearchResponse{}, fmt.Errorf("engine: Search before Load")
	}

	q, err := e.buildQuery(req)
	if err != nil {
		return SearchResponse{}, err
	}

	limit := req.Limit
	if limit <= 0 || limit > MaxLimit {
		limit = MaxLimit
	}

	offset := max(req.Offset, 0)

	resp := SearchResponse{
		States: map[string]int{},
		Offset: offset,
		Items:  []Item{},
	}

	for _, i := range e.order {
		row := &e.rows[i]

		if !req.IncludeClosed && row.state != corpus.StateOpen && row.state != corpus.StateStale {
			continue
		}

		if !q.Match(&row.posting) {
			continue
		}

		if resp.Matched >= offset && len(resp.Items) < limit {
			resp.Items = append(resp.Items, e.item(row))
		}

		resp.Matched++
		resp.States[row.state.String()]++
	}

	return resp, nil
}

// SearchJSON is [Engine.Search] with JSON at both ends, which is the shape the
// wasm bridge wants: one string in, one string out, no per-field JS traffic.
func (e *Engine) SearchJSON(data []byte) ([]byte, error) {
	var req SearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("engine: decode search request: %w", err)
	}

	resp, err := e.Search(req)
	if err != nil {
		return nil, err
	}

	return json.Marshal(resp)
}

// buildQuery maps the wire request onto the shared vocabulary. The enum fields
// go through the same normalizers the adapters use, so "Full-Time" typed into
// a URL means what it means everywhere else, and an unrecognisable value is an
// error rather than a silent zero matches.
func (e *Engine) buildQuery(req SearchRequest) (query.Query, error) {
	q := query.Query{
		Titles:          req.Titles,
		ExcludeTitles:   req.ExcludeTitles,
		Locations:       req.Locations,
		Companies:       req.Companies,
		Departments:     req.Departments,
		Remote:          req.Remote,
		HasCompensation: req.HasCompensation,
		MinAnnual:       req.MinAnnual,
	}

	for _, raw := range req.EmploymentTypes {
		if strings.TrimSpace(raw) == "" {
			continue
		}

		typ, ok := jobposting.NormalizeEmploymentType(raw)
		if !ok {
			return query.Query{}, fmt.Errorf("engine: unknown employment type %q", raw)
		}

		q.EmploymentTypes = append(q.EmploymentTypes, typ)
	}

	for _, raw := range req.WorkplaceTypes {
		if strings.TrimSpace(raw) == "" {
			continue
		}

		typ, ok := jobposting.NormalizeWorkplaceType(raw)
		if !ok {
			return query.Query{}, fmt.Errorf("engine: unknown workplace type %q", raw)
		}

		q.WorkplaceTypes = append(q.WorkplaceTypes, typ)
	}

	if req.PostedSinceDays > 0 {
		q.PostedSince = e.now.Add(-time.Duration(req.PostedSinceDays) * 24 * time.Hour)
	}

	return q, nil
}

func (e *Engine) item(row *record) Item {
	p := &row.posting

	item := Item{
		Title:          p.Title,
		Company:        p.Company,
		Location:       p.Location,
		URL:            p.URL,
		Platform:       p.Source.Platform,
		Department:     p.Department,
		Team:           p.Team,
		EmploymentType: string(p.EmploymentType),
		WorkplaceType:  string(p.WorkplaceType),
		Seniority:      p.Seniority,
		Remote:         p.IsRemote(),
		Compensation:   compensationLabel(p.Compensation),
		State:          row.state.String(),
	}

	if !p.PostedAt.IsZero() {
		item.PostedAt = p.PostedAt.UTC().Format(time.RFC3339)
	}

	if !row.firstSeen.IsZero() {
		item.FirstSeen = row.firstSeen.UTC().Format(time.RFC3339)
	}

	return item
}

// compensationLabel renders a published pay range for the results list. The
// board's own summary wins when it wrote one; otherwise the range is formatted
// from its parts. A nil Compensation is the common case and renders as
// nothing, never as a guess.
func compensationLabel(c *jobposting.Compensation) string {
	if c == nil || c.IsZero() {
		return ""
	}

	if c.Summary != "" {
		return c.Summary
	}

	var amount string

	switch {
	case c.Min > 0 && c.Max > 0 && c.Min != c.Max:
		amount = formatAmount(c.Min) + "–" + formatAmount(c.Max)
	case c.Max > 0:
		amount = formatAmount(c.Max)
	case c.Min > 0:
		amount = formatAmount(c.Min)
	default:
		return ""
	}

	if c.Currency != "" {
		amount += " " + c.Currency
	}

	if c.Period != jobposting.PeriodUnknown {
		amount += " / " + string(c.Period)
	}

	return amount
}

// formatAmount renders a pay figure with thousands separators and no invented
// precision: whole numbers stay whole, fractional ones keep two places.
func formatAmount(f float64) string {
	if f != math.Trunc(f) {
		return fmt.Sprintf("%.2f", f)
	}

	digits := fmt.Sprintf("%.0f", f)
	if len(digits) <= 3 {
		return digits
	}

	var b strings.Builder

	lead := len(digits) % 3
	if lead > 0 {
		b.WriteString(digits[:lead])
	}

	for i := lead; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}

		b.WriteString(digits[i : i+3])
	}

	return b.String()
}
