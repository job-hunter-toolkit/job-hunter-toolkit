package engine_test

import (
	"context"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/engine"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/internal/testcorpus"
	"github.com/shoenig/test/must"
)

// open builds the shared fixture generation in a temp dir and loads an engine
// over it, pinned to the fixture's clock so lifecycle states cannot drift with
// real time.
func open(t *testing.T) *engine.Engine {
	t.Helper()

	dir := t.TempDir()
	ctx := t.Context()

	must.NoError(t, testcorpus.Build(ctx, dir, 1))

	e, err := engine.Open(ctx, corpus.DirStore{Dir: dir}, testcorpus.Now)
	must.NoError(t, err)
	must.NoError(t, e.Load(ctx))

	return e
}

func TestSummaryReportsTheGenerationHonestly(t *testing.T) {
	e := open(t)
	want := testcorpus.Expect()

	s := e.Summary()
	must.Eq(t, 3, int(s.Generation)) // three folds published three generations
	must.Eq(t, want.Rows, s.Rows)
	must.Eq(t, want.Open, s.Open)
	must.Eq(t, want.Stale, s.Stale)
	must.Eq(t, want.Closed, s.Closed)
	must.Eq(t, want.Lapsed, s.Lapsed)
	must.False(t, s.Partial)
	must.Eq(t, "2026-07-29T06:00:00Z", s.RunAt) // run3, the producing run
	must.Eq(t, 6.0, s.AgeHours)
}

// search is a helper asserting how many rows a request matches.
func search(t *testing.T, e *engine.Engine, req engine.SearchRequest) engine.SearchResponse {
	t.Helper()

	resp, err := e.Search(req)
	must.NoError(t, err)

	return resp
}

func TestSearchDefaultsToRowsCurrentlyBelievedOpen(t *testing.T) {
	e := open(t)

	resp := search(t, e, engine.SearchRequest{})
	must.Eq(t, 6, resp.Matched) // 4 open + 2 stale; closed and lapsed excluded
	must.Eq(t, 4, resp.States["open"])
	must.Eq(t, 2, resp.States["stale"])
	must.Eq(t, 0, resp.States["closed"])

	all := search(t, e, engine.SearchRequest{IncludeClosed: true})
	must.Eq(t, 8, all.Matched)
	must.Eq(t, 1, all.States["closed"])
	must.Eq(t, 1, all.States["lapsed"])
}

func TestSearchFacetsCountMatchedRowsExactly(t *testing.T) {
	e := open(t)

	resp := search(t, e, engine.SearchRequest{IncludeFacets: true})
	must.Eq(t, "rows", resp.CountUnit)
	must.NotNil(t, resp.Facets)
	must.Eq(t, resp.Matched, facetTotal(resp.Facets.Employment))
	must.Eq(t, resp.Matched, facetTotal(resp.Facets.Workplace))
	must.Eq(t, resp.Matched, facetTotal(resp.Facets.Compensation))
	must.Eq(t, resp.Matched, facetTotal(resp.Facets.PostedAge))
	must.Eq(t, resp.Matched, facetTotal(resp.Facets.FirstSeenAge))

	must.Eq(t, 2, facetRows(resp.Facets.Employment, "full_time"))
	must.Eq(t, 1, facetRows(resp.Facets.Employment, "contract"))
	must.Eq(t, 3, facetRows(resp.Facets.Employment, "unknown"))
	must.Eq(t, 2, facetRows(resp.Facets.Workplace, "remote"))
	must.Eq(t, 1, facetRows(resp.Facets.Workplace, "hybrid"))
	must.Eq(t, 1, facetRows(resp.Facets.Workplace, "onsite"))
	must.Eq(t, 2, facetRows(resp.Facets.Workplace, "unknown"))
	must.Eq(t, 2, facetRows(resp.Facets.Compensation, "annual"))
	must.Eq(t, 4, facetRows(resp.Facets.Compensation, "undisclosed"))
	must.Eq(t, 2, facetRows(resp.Facets.PostedAge, "7d"))
	must.Eq(t, 2, facetRows(resp.Facets.PostedAge, "30d"))
	must.Eq(t, 2, facetRows(resp.Facets.PostedAge, "unknown"))
}

func TestSearchFacetsRespectFiltersAndLifecycle(t *testing.T) {
	e := open(t)

	filtered := search(t, e, engine.SearchRequest{
		Titles: []string{"engineer"}, IncludeFacets: true,
	})
	must.Eq(t, 3, filtered.Matched)
	must.Eq(t, 2, facetRows(filtered.Facets.Employment, "full_time"))
	must.Eq(t, 1, facetRows(filtered.Facets.Employment, "unknown"))

	all := search(t, e, engine.SearchRequest{IncludeClosed: true, IncludeFacets: true})
	must.Eq(t, 8, all.Matched)
	for _, dimension := range [][]engine.Facet{
		all.Facets.Employment, all.Facets.Workplace, all.Facets.Compensation,
		all.Facets.PostedAge, all.Facets.FirstSeenAge,
	} {
		must.Eq(t, all.Matched, facetTotal(dimension))
	}
}

func TestSearchFacetsAreOptInAndCancellationStopsScan(t *testing.T) {
	e := open(t)

	resp := search(t, e, engine.SearchRequest{})
	must.Nil(t, resp.Facets)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := e.SearchContext(ctx, engine.SearchRequest{IncludeFacets: true})
	must.ErrorIs(t, err, context.Canceled)
}

func facetRows(values []engine.Facet, value string) int {
	for _, facet := range values {
		if facet.Value == value {
			return facet.Rows
		}
	}

	return -1
}

func facetTotal(values []engine.Facet) int {
	total := 0
	for _, facet := range values {
		total += facet.Rows
	}

	return total
}

func TestSearchSpeaksTheSharedQueryVocabulary(t *testing.T) {
	e := open(t)

	cases := []struct {
		name string
		req  engine.SearchRequest
		want int
	}{
		{"title substring", engine.SearchRequest{Titles: []string{"engineer"}}, 3},
		{"exclude title", engine.SearchRequest{ExcludeTitles: []string{"manager"}}, 5},
		{"location", engine.SearchRequest{Locations: []string{"remote"}}, 2},
		{"company", engine.SearchRequest{Companies: []string{"acme"}}, 4},
		{"department or team", engine.SearchRequest{Departments: []string{"security"}}, 1},
		{"remote heuristic", engine.SearchRequest{Remote: true}, 2},
		{"has pay", engine.SearchRequest{HasCompensation: true}, 2},
		{"pay floor", engine.SearchRequest{MinAnnual: 200000}, 1},
		{"employment type", engine.SearchRequest{EmploymentTypes: []string{"full_time"}}, 2},
		{"employment type in a board's spelling", engine.SearchRequest{EmploymentTypes: []string{"Full-Time"}}, 2},
		{"workplace type with fallback", engine.SearchRequest{WorkplaceTypes: []string{"remote"}}, 2},
		{"posted since", engine.SearchRequest{PostedSinceDays: 7}, 2},
		{"conjunction", engine.SearchRequest{Titles: []string{"engineer"}, Remote: true}, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			must.Eq(t, tc.want, search(t, e, tc.req).Matched)
		})
	}
}

func TestSearchRejectsAnUnknownEnumValue(t *testing.T) {
	e := open(t)

	_, err := e.Search(engine.SearchRequest{EmploymentTypes: []string{"gibberish"}})
	must.ErrorContains(t, err, "unknown employment type")

	_, err = e.Search(engine.SearchRequest{WorkplaceTypes: []string{"underwater"}})
	must.ErrorContains(t, err, "unknown workplace type")
}

func TestSearchPagesDeterministically(t *testing.T) {
	e := open(t)

	first := search(t, e, engine.SearchRequest{Limit: 2})
	must.Eq(t, 6, first.Matched)
	must.Len(t, 2, first.Items)

	second := search(t, e, engine.SearchRequest{Limit: 2, Offset: 2})
	must.Eq(t, 6, second.Matched)
	must.Len(t, 2, second.Items)
	must.NotEq(t, first.Items[0].Title, second.Items[0].Title)

	// Newest posted date first: Data Scientist (07-27) precedes Senior
	// Software Engineer (07-25); undated rows sort last.
	must.Eq(t, "Data Scientist", first.Items[0].Title)
	must.Eq(t, "Senior Software Engineer", first.Items[1].Title)

	last := search(t, e, engine.SearchRequest{Offset: 4})
	must.Len(t, 2, last.Items)
	must.Eq(t, "", last.Items[1].PostedAt)
}

func TestItemsCarryStateAndCompensation(t *testing.T) {
	e := open(t)

	resp := search(t, e, engine.SearchRequest{Titles: []string{"senior software"}})
	must.Len(t, 1, resp.Items)

	item := resp.Items[0]
	must.Eq(t, "open", item.State)
	must.Eq(t, "150,000–180,000 USD / year", item.Compensation)
	must.Eq(t, "greenhouse", item.Platform)
	must.True(t, item.Remote)

	stale := search(t, e, engine.SearchRequest{Companies: []string{"globex"}})
	must.Eq(t, 2, stale.Matched)

	for _, it := range stale.Items {
		must.Eq(t, "stale", it.State)
	}
}

func TestSearchJSONRoundTrips(t *testing.T) {
	e := open(t)

	out, err := e.SearchJSON([]byte(`{"titles":["engineer"],"limit":1}`))
	must.NoError(t, err)
	must.StrContains(t, string(out), `"matched":3`)

	_, err = e.SearchJSON([]byte(`{"titles":`))
	must.ErrorContains(t, err, "decode search request")
}

func TestSearchBeforeLoadRefuses(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()

	must.NoError(t, testcorpus.Build(ctx, dir, 1))

	e, err := engine.Open(ctx, corpus.DirStore{Dir: dir}, testcorpus.Now)
	must.NoError(t, err)

	_, err = e.Search(engine.SearchRequest{})
	must.ErrorContains(t, err, "before Load")
}
