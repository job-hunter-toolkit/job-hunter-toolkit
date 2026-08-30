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
	"runtime"
	"slices"
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

	platform, company, url, title, location   stringColumn
	department, team, employment, workplace   stringColumn
	seniority, compensation, currency, period stringColumn
	compensations                             []compensationRecord

	// order is row indexes in presentation order, computed once at load:
	// PostedAt descending with undated rows last, then FirstSeen descending,
	// then company and title ascending, then table row index so the order is
	// total and a reload shows the same page.
	order []uint32
}

// record keeps only fixed-width query state. Strings live in interned columns:
// generation 11 has two million rows, and one 16-byte Go string header per
// field per row used 1.73 GiB before any browser overhead. Four-byte IDs keep
// the resident index within mobile browser memory without changing the corpus.
type record struct {
	firstSeen, postedAt int64
	comp                uint32
	state               corpus.State
	isRemote, isHybrid  bool
}

type compensationRecord struct {
	minimum, maximum, annualMax float64
	hasAnnual                   bool
}

type stringColumn struct {
	ids    []uint32
	values []string
	folded []string
	direct []string
}

func (c stringColumn) at(i int) string {
	if c.direct != nil {
		return c.direct[i]
	}
	return c.values[c.ids[i]]
}

func (c stringColumn) fold(i int) string {
	if len(c.folded) == 0 {
		return c.at(i)
	}
	return c.folded[c.ids[i]]
}

func loadStringColumn(table *corpus.Table, name string, folded bool) (stringColumn, error) {
	dictionary, ids, indexed, err := table.StringDictionary(name)
	if err != nil {
		return stringColumn{}, err
	}
	if indexed {
		column := stringColumn{ids: ids, values: dictionary}
		if folded {
			column.folded = make([]string, len(dictionary))
			for i, value := range dictionary {
				column.folded[i] = strings.ToLower(value)
			}
		}
		return column, nil
	}

	raw, err := table.Strings(name)
	if err != nil {
		return stringColumn{}, err
	}

	column := stringColumn{ids: make([]uint32, len(raw))}
	internIDs := make(map[string]uint32)
	for i, value := range raw {
		id, ok := internIDs[value]
		if !ok {
			id = uint32(len(column.values))
			internIDs[value] = id
			column.values = append(column.values, value)
			if folded {
				column.folded = append(column.folded, strings.ToLower(value))
			}
		}
		column.ids[i] = id
	}

	return column, nil
}

func loadDirectStringColumn(table *corpus.Table, name string) (stringColumn, error) {
	values, err := table.Strings(name)
	return stringColumn{direct: values}, err
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

	// IncludeFacets asks the scan to count a bounded set of point-in-time
	// dimensions over every matching row. It adds no columns or second pass and
	// is opt-in so saved-search rollups and paging do not pay for unused counts.
	IncludeFacets bool `json:"include_facets,omitempty"`

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
	// Matched counts every corpus row satisfying the query, not just the page.
	// It is not a deduplicated listing count: historical rows can share a
	// dedupe key. The manifest Summary.Open is the deduplicated believed-open
	// listing count for the whole generation.
	Matched   int    `json:"matched"`
	CountUnit string `json:"count_unit"`

	// States counts the matched rows by lifecycle state, so "1,204 matches"
	// can honestly read "1,180 open, 24 stale".
	States map[string]int `json:"states"`

	Offset int     `json:"offset"`
	Items  []Item  `json:"items"`
	Facets *Facets `json:"facets,omitempty"`
}

// DetailResponse resolves an exact posting URL inside the opened snapshot.
// URLs are stable locators only within one generation. Matches is a row count:
// historical rows can legitimately repeat a URL, and the first item follows
// the same deterministic newest-first order as Search.
type DetailResponse struct {
	Found     bool   `json:"found"`
	Matches   int    `json:"matches"`
	CountUnit string `json:"count_unit"`
	Item      *Item  `json:"item,omitempty"`
}

// Facet is one stable machine value and its exact matching row count.
type Facet struct {
	Value string `json:"value"`
	Rows  int    `json:"rows"`
}

// Facets is a compact point-in-time overview of the matched rows. Every
// dimension has fixed cardinality, including an explicit unknown bucket, so
// malformed or unexpectedly diverse source data cannot grow query memory.
// Age buckets are mutually exclusive and measured against the engine's pinned
// clock: 7d means [now-7d, now], 30d means older than 7d through 30d.
type Facets struct {
	Employment   []Facet `json:"employment"`
	Workplace    []Facet `json:"workplace"`
	Compensation []Facet `json:"compensation"`
	PostedAge    []Facet `json:"posted_age"`
	FirstSeenAge []Facet `json:"first_seen_age"`
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
	e.rows = rows

	// Keep four-byte IDs per row and one copy of each distinct value. This is
	// the same logical projection as before, but does not spend 16 bytes on a Go
	// string header for every field of every row.
	for _, column := range []struct {
		name   string
		folded bool
		to     *stringColumn
	}{
		{colPlatform, false, &e.platform},
		{colCompany, true, &e.company},
		{colTitle, true, &e.title},
		{colLocation, true, &e.location},
		{colDepartment, true, &e.department},
		{colTeam, true, &e.team},
		{colEmployment, false, &e.employment},
		{colWorkplace, false, &e.workplace},
		{colSeniority, false, &e.seniority},
	} {
		loaded, err := loadStringColumn(table, column.name, column.folded)
		if err != nil {
			return err
		}
		*column.to = loaded
		runtime.GC()
	}
	var err error
	if e.url, err = loadDirectStringColumn(table, colURL); err != nil {
		return err
	}
	runtime.GC()

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
		rows[i].firstSeen = firstSeen[i]
		rows[i].postedAt = postedAt[i]

		switch remote[i] {
		case remoteFalse:
			rows[i].isRemote = false
		case remoteTrue:
			rows[i].isRemote = true
		}
	}
	firstSeen = nil
	postedAt = nil
	runtime.GC()

	if err := e.loadCompensation(table); err != nil {
		return err
	}
	runtime.GC()

	if err := e.loadStates(table, remote); err != nil {
		return err
	}
	remote = nil
	runtime.GC()

	order := make([]uint32, len(rows))
	for i := range order {
		order[i] = uint32(i)
	}

	sort.SliceStable(order, func(x, y int) bool {
		ai, bi := int(order[x]), int(order[y])
		a, b := &rows[ai], &rows[bi]

		// Dated postings first, newest first. An undated posting is not old,
		// it is undated, but a list has to put it somewhere and burying it
		// beats letting it pretend to be today's.
		if a.postedAt != b.postedAt {
			if a.postedAt == 0 || b.postedAt == 0 {
				return b.postedAt == 0
			}
			return a.postedAt > b.postedAt
		}

		if a.firstSeen != b.firstSeen {
			return a.firstSeen > b.firstSeen
		}

		if e.company.at(ai) != e.company.at(bi) {
			return e.company.at(ai) < e.company.at(bi)
		}

		if e.title.at(ai) != e.title.at(bi) {
			return e.title.at(ai) < e.title.at(bi)
		}

		// The table's row order is itself deterministic, so the index is a
		// stable final tiebreak.
		return order[x] < order[y]
	})

	e.order = order

	return nil
}

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
func (e *Engine) loadCompensation(table *corpus.Table) error {
	present, err := table.Ints(colComp)
	if err != nil {
		return err
	}

	minimums, err := loadStringColumn(table, colCompMin, false)
	if err != nil {
		return err
	}
	maximums, err := loadStringColumn(table, colCompMax, false)
	if err != nil {
		return err
	}
	if e.currency, err = loadStringColumn(table, colCompCcy, false); err != nil {
		return err
	}
	if e.period, err = loadStringColumn(table, colCompPeriod, false); err != nil {
		return err
	}
	if e.compensation, err = loadStringColumn(table, colCompSummary, false); err != nil {
		return err
	}

	e.compensations = append(e.compensations, compensationRecord{})
	for i := range e.rows {
		if present[i] == 0 {
			continue
		}

		minimum, err := decodeFloat(minimums.at(i))
		if err != nil {
			return fmt.Errorf("engine: row %d compensation_min: %w", i, err)
		}

		maximum, err := decodeFloat(maximums.at(i))
		if err != nil {
			return fmt.Errorf("engine: row %d compensation_max: %w", i, err)
		}

		compensation := &jobposting.Compensation{
			Min:      minimum,
			Max:      maximum,
			Currency: e.currency.at(i),
			Period:   jobposting.Period(e.period.at(i)),
			Summary:  e.compensation.at(i),
		}
		if compensation.IsZero() {
			continue
		}
		annualMax, hasAnnual := compensation.AnnualMax()
		e.rows[i].comp = uint32(len(e.compensations))
		e.compensations = append(e.compensations, compensationRecord{
			minimum: minimum, maximum: maximum, annualMax: annualMax, hasAnnual: hasAnnual,
		})
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
func (e *Engine) loadStates(table *corpus.Table, remote []int64) error {
	sourceKeys, err := loadStringColumn(table, colSourceKey, false)
	if err != nil {
		return err
	}
	reasons, err := loadStringColumn(table, colClosedWhy, false)
	if err != nil {
		return err
	}

	for i := range e.rows {
		synthetic := corpus.Row{Posting: jobposting.JobPosting{Source: jobposting.PostingSource{
			Platform: e.platform.at(i),
			Key:      sourceKeys.at(i),
		}}}
		if reason := reasons.at(i); reason != "" {
			synthetic.Closed = &corpus.Closure{Reason: reason}
		}

		r := &e.rows[i]
		r.state = e.corpus.State(synthetic, e.now)
		if remote[i] == 0 {
			r.isRemote = jobposting.LooksRemote(e.location.fold(i), e.title.fold(i))
		}
		r.isHybrid = jobposting.LooksHybrid(e.location.fold(i), e.title.fold(i))
	}

	return nil
}

// Loaded reports whether Load has run.
func (e *Engine) Loaded() bool { return e.rows != nil }

// Search evaluates a request against the loaded rows.
func (e *Engine) Search(req SearchRequest) (SearchResponse, error) {
	return e.SearchContext(context.Background(), req)
}

// SearchContext evaluates a request and stops promptly when ctx is cancelled.
func (e *Engine) SearchContext(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return e.searchContext(ctx, req, nil)
}

// SearchYielding evaluates a request while calling yield between bounded
// chunks. The Wasm bridge uses it to return control to the worker event loop,
// where a cancellation message can run; native callers should use SearchContext.
func (e *Engine) SearchYielding(ctx context.Context, req SearchRequest, yield func() error) (SearchResponse, error) {
	return e.searchContext(ctx, req, yield)
}

// DetailYielding resolves an exact URL without building a second index. It
// scans the same compact URL column Search renders and yields on the same
// cadence so browser cancellation stays prompt.
func (e *Engine) DetailYielding(ctx context.Context, url string, yield func() error) (DetailResponse, error) {
	if e.rows == nil {
		return DetailResponse{}, fmt.Errorf("engine: Detail before Load")
	}

	resp := DetailResponse{CountUnit: "rows"}
	for n, rawIndex := range e.order {
		if n&1023 == 0 {
			select {
			case <-ctx.Done():
				return DetailResponse{}, ctx.Err()
			default:
			}
		}
		if yield != nil && n > 0 && n&32767 == 0 {
			if err := yield(); err != nil {
				return DetailResponse{}, err
			}
			if err := ctx.Err(); err != nil {
				return DetailResponse{}, err
			}
		}

		i := int(rawIndex)
		if e.url.at(i) != url {
			continue
		}
		resp.Matches++
		if resp.Item == nil {
			item := e.item(i)
			resp.Item = &item
		}
	}
	resp.Found = resp.Item != nil

	return resp, nil
}

func (e *Engine) searchContext(ctx context.Context, req SearchRequest, yield func() error) (SearchResponse, error) {
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
		States:    map[string]int{},
		Offset:    offset,
		Items:     []Item{},
		CountUnit: "rows",
	}
	if req.IncludeFacets {
		resp.Facets = newFacets()
	}

	c := compileQuery(q)

	for n, rawIndex := range e.order {
		i := int(rawIndex)
		if n&1023 == 0 {
			select {
			case <-ctx.Done():
				return SearchResponse{}, ctx.Err()
			default:
			}
		}
		if yield != nil && n > 0 && n&32767 == 0 {
			if err := yield(); err != nil {
				return SearchResponse{}, err
			}
			if err := ctx.Err(); err != nil {
				return SearchResponse{}, err
			}
		}

		row := &e.rows[i]

		if !req.IncludeClosed && row.state != corpus.StateOpen && row.state != corpus.StateStale {
			continue
		}

		if !c.match(e, i) {
			continue
		}

		if resp.Matched >= offset && len(resp.Items) < limit {
			resp.Items = append(resp.Items, e.item(i))
		}

		resp.Matched++
		resp.States[row.state.String()]++
		if resp.Facets != nil {
			resp.Facets.add(e, i)
		}
	}

	return resp, nil
}

func newFacets() *Facets {
	values := func(raw ...string) []Facet {
		out := make([]Facet, len(raw))
		for i, value := range raw {
			out[i].Value = value
		}
		return out
	}

	return &Facets{
		Employment:   values("full_time", "part_time", "contract", "internship", "temporary", "volunteer", "unknown"),
		Workplace:    values("remote", "hybrid", "onsite", "unknown"),
		Compensation: values("annual", "other", "undisclosed"),
		PostedAge:    values("7d", "30d", "older", "unknown"),
		FirstSeenAge: values("7d", "30d", "older", "unknown"),
	}
}

func increment(values []Facet, value string) {
	for i := range values {
		if values[i].Value == value {
			values[i].Rows++
			return
		}
	}

	values[len(values)-1].Rows++
}

func (f *Facets) add(e *Engine, i int) {
	row := &e.rows[i]
	employment := e.employment.at(i)
	if employment == "" {
		employment = "unknown"
	}
	increment(f.Employment, employment)

	workplace := e.workplace.at(i)
	if workplace == "" {
		switch {
		case row.isHybrid:
			workplace = "hybrid"
		case row.isRemote:
			workplace = "remote"
		default:
			workplace = "unknown"
		}
	}
	increment(f.Workplace, workplace)

	compensation := "undisclosed"
	comp := e.compensations[row.comp]
	if comp.hasAnnual {
		compensation = "annual"
	} else if row.comp != 0 {
		compensation = "other"
	}
	increment(f.Compensation, compensation)
	increment(f.PostedAge, ageBucket(decodeTime(row.postedAt), e.now))
	increment(f.FirstSeenAge, ageBucket(decodeTime(row.firstSeen), e.now))
}

func ageBucket(value, now time.Time) string {
	if value.IsZero() || value.After(now) {
		return "unknown"
	}

	age := now.Sub(value)
	if age <= 7*24*time.Hour {
		return "7d"
	}
	if age <= 30*24*time.Hour {
		return "30d"
	}
	return "older"
}

// compiledQuery is [query.Query] with every term folded once, evaluated
// against the folds and flags [Engine.Load] precomputed per record.
//
// This exists because [query.Query.Match] lowercases the haystack and every
// term per posting, which is correct for a streaming crawl and ruinous for a
// resident scan: over 1.3 million rows a remote-only query measured 3.1 s and
// a title term 1.3 s of pure refolding, all of it on the browser's main
// thread. The semantics here must be exactly Match's, no more and no less;
// TestSearchMatchesQueryMatch holds the two together row for row.
type compiledQuery struct {
	remote      bool
	hasComp     bool
	minAnnual   float64
	postedSince time.Time

	titles        []string
	excludeTitles []string
	locations     []string
	companies     []string
	departments   []string

	employment []jobposting.EmploymentType
	workplace  []jobposting.WorkplaceType
}

func compileQuery(q query.Query) compiledQuery {
	return compiledQuery{
		remote:        q.Remote,
		hasComp:       q.HasCompensation,
		minAnnual:     q.MinAnnual,
		postedSince:   q.PostedSince,
		titles:        foldTerms(q.Titles),
		excludeTitles: foldTerms(q.ExcludeTitles),
		locations:     foldTerms(q.Locations),
		companies:     foldTerms(q.Companies),
		departments:   foldTerms(q.Departments),
		employment:    usableEnums(q.EmploymentTypes),
		workplace:     usableEnums(q.WorkplaceTypes),
	}
}

// foldTerms lowercases and trims once, dropping blanks — the same treatment
// containsAny gives each term per row, hoisted to per query. A list that folds
// to nothing is no constraint, mirroring hasUsableTerm.
func foldTerms(terms []string) []string {
	out := make([]string, 0, len(terms))

	for _, term := range terms {
		if folded := strings.ToLower(strings.TrimSpace(term)); folded != "" {
			out = append(out, folded)
		}
	}

	return out
}

// usableEnums drops zero values, mirroring hasUsableEnum: an empty entry is a
// mistake to ignore, not a filter for postings whose board said nothing.
func usableEnums[T ~string](values []T) []T {
	out := make([]T, 0, len(values))

	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}

// match mirrors [query.Query.Match] clause for clause over the precomputed
// record. Any semantic change to Match belongs there first; the parity test
// will fail here until this follows.
func (c *compiledQuery) match(e *Engine, i int) bool {
	r := &e.rows[i]
	if c.remote && !r.isRemote {
		return false
	}

	if c.hasComp && r.comp == 0 {
		return false
	}

	if c.minAnnual > 0 {
		compensation := e.compensations[r.comp]
		if !compensation.hasAnnual || compensation.annualMax < c.minAnnual {
			return false
		}
	}

	if len(c.titles) > 0 && !anyContains(e.title.fold(i), c.titles) {
		return false
	}

	if len(c.excludeTitles) > 0 && anyContains(e.title.fold(i), c.excludeTitles) {
		return false
	}

	if len(c.locations) > 0 && !anyContains(e.location.fold(i), c.locations) {
		return false
	}

	if len(c.companies) > 0 && !anyContains(e.company.fold(i), c.companies) {
		return false
	}

	if len(c.departments) > 0 &&
		!anyContains(e.department.fold(i), c.departments) &&
		!anyContains(e.team.fold(i), c.departments) {
		return false
	}

	if len(c.employment) > 0 {
		employment := jobposting.EmploymentType(e.employment.at(i))
		if employment == jobposting.EmploymentTypeUnknown ||
			!slices.Contains(c.employment, employment) {
			return false
		}
	}

	if len(c.workplace) > 0 && !c.matchesWorkplace(e, i) {
		return false
	}

	if !c.postedSince.IsZero() {
		postedAt := decodeTime(r.postedAt)
		if postedAt.IsZero() || postedAt.Before(c.postedSince) {
			return false
		}
	}

	return true
}

func (c *compiledQuery) matchesWorkplace(e *Engine, i int) bool {
	r := &e.rows[i]
	workplace := jobposting.WorkplaceType(e.workplace.at(i))
	if workplace != jobposting.WorkplaceTypeUnknown {
		return slices.Contains(c.workplace, workplace)
	}

	for _, want := range c.workplace {
		switch want {
		case jobposting.WorkplaceTypeRemote:
			if r.isRemote {
				return true
			}
		case jobposting.WorkplaceTypeHybrid:
			if r.isHybrid {
				return true
			}
		}
	}

	return false
}

func anyContains(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}

	return false
}

// SearchJSON is [Engine.Search] with JSON at both ends, which is the shape the
// wasm bridge wants: one string in, one string out, no per-field JS traffic.
func (e *Engine) SearchJSON(data []byte) ([]byte, error) {
	return e.SearchJSONContext(context.Background(), data)
}

// SearchJSONContext is SearchJSON with cancellation for the worker bridge.
func (e *Engine) SearchJSONContext(ctx context.Context, data []byte) ([]byte, error) {
	return e.searchJSON(ctx, data, nil)
}

// SearchJSONYielding is SearchJSONContext with bounded yielding for Wasm.
func (e *Engine) SearchJSONYielding(ctx context.Context, data []byte, yield func() error) ([]byte, error) {
	return e.searchJSON(ctx, data, yield)
}

// DetailJSONYielding is the Wasm wire shape for DetailYielding.
func (e *Engine) DetailJSONYielding(ctx context.Context, url string, yield func() error) ([]byte, error) {
	resp, err := e.DetailYielding(ctx, url, yield)
	if err != nil {
		return nil, err
	}

	return json.Marshal(resp)
}

func (e *Engine) searchJSON(ctx context.Context, data []byte, yield func() error) ([]byte, error) {
	var req SearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("engine: decode search request: %w", err)
	}

	resp, err := e.SearchYielding(ctx, req, yield)
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

func (e *Engine) item(i int) Item {
	row := &e.rows[i]
	p := e.posting(i)
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
		Remote:         row.isRemote,
		Compensation:   compensationLabel(p.Compensation),
		State:          row.state.String(),
	}

	if row.postedAt != 0 {
		item.PostedAt = decodeTime(row.postedAt).Format(time.RFC3339)
	}

	if row.firstSeen != 0 {
		item.FirstSeen = decodeTime(row.firstSeen).Format(time.RFC3339)
	}

	return item
}

func (e *Engine) posting(i int) jobposting.JobPosting {
	row := &e.rows[i]
	remote := row.isRemote
	p := jobposting.JobPosting{
		Title: e.title.at(i), Company: e.company.at(i), Location: e.location.at(i), URL: e.url.at(i),
		Department: e.department.at(i), Team: e.team.at(i), Seniority: e.seniority.at(i),
		EmploymentType: jobposting.EmploymentType(e.employment.at(i)),
		WorkplaceType:  jobposting.WorkplaceType(e.workplace.at(i)),
		PostedAt:       decodeTime(row.postedAt), Remote: &remote,
		Source: jobposting.PostingSource{Platform: e.platform.at(i)},
	}
	if row.comp != 0 {
		comp := e.compensations[row.comp]
		p.Compensation = &jobposting.Compensation{
			Min: comp.minimum, Max: comp.maximum, Currency: e.currency.at(i),
			Period: jobposting.Period(e.period.at(i)), Summary: e.compensation.at(i),
		}
	}
	return p
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
