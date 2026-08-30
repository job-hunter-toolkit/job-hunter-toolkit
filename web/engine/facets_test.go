package engine

import (
	"context"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test/must"
)

func TestFacetsBoundMalformedValuesAndFutureDates(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	e := &Engine{
		now:           now,
		rows:          []record{{postedAt: now.Add(time.Hour).UnixMilli(), firstSeen: now.Add(time.Hour).UnixMilli(), futureDate: true}},
		compensations: []compensationRecord{{}},
		employment:    testStringColumn("unexpected-employment-value"),
		workplace:     testStringColumn("unexpected-workplace-value"),
	}

	facets := newFacets()
	facets.add(e, 0)

	must.Eq(t, 1, facetCount(facets.Employment, "unknown"))
	must.Eq(t, 1, facetCount(facets.Workplace, "unknown"))
	must.Eq(t, 1, facetCount(facets.PostedAge, "unknown"))
	must.Eq(t, 1, facetCount(facets.FirstSeenAge, "unknown"))
}

func TestFacetCountingHasConstantMemory(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	e := &Engine{
		now:           now,
		rows:          []record{{postedAt: now.UnixMilli(), firstSeen: now.UnixMilli()}},
		compensations: []compensationRecord{{}},
		employment:    testStringColumn(string(jobposting.EmploymentTypeFullTime)),
		workplace:     testStringColumn(string(jobposting.WorkplaceTypeRemote)),
	}
	facets := newFacets()

	allocations := testing.AllocsPerRun(1000, func() {
		facets.add(e, 0)
	})
	must.Eq(t, 0.0, allocations)

	// These fixed cardinalities are the query-memory budget. Adding a
	// high-cardinality dimension must use a separately measured index rather
	// than turning this scan into a map proportional to corpus variety.
	must.Len(t, 7, facets.Employment)
	must.Len(t, 4, facets.Workplace)
	must.Len(t, 3, facets.Compensation)
	must.Len(t, 4, facets.PostedAge)
	must.Len(t, 4, facets.FirstSeenAge)
}

func TestSearchYieldingLetsCancellationRunBetweenChunks(t *testing.T) {
	const rows = 32769
	e := &Engine{
		rows:       make([]record, rows),
		order:      make([]uint32, rows),
		employment: testStringColumn(""),
		workplace:  testStringColumn(""),
		title:      testStringColumn(""), location: testStringColumn(""),
		company: testStringColumn(""), department: testStringColumn(""), team: testStringColumn(""),
	}
	for i := range e.order {
		e.order[i] = uint32(i)
	}

	ctx, cancel := context.WithCancel(t.Context())
	yields := 0
	_, err := e.SearchYielding(ctx, SearchRequest{Offset: rows}, func() error {
		yields++
		cancel()
		return nil
	})

	must.ErrorIs(t, err, context.Canceled)
	must.Eq(t, 1, yields)
}

func TestDetailYieldingLetsCancellationRunBetweenChunks(t *testing.T) {
	const rows = 32769
	e := &Engine{
		rows:  make([]record, rows),
		order: make([]uint32, rows),
		url:   stringColumn{direct: make([]string, rows)},
	}
	for i := range e.order {
		e.order[i] = uint32(i)
	}

	ctx, cancel := context.WithCancel(t.Context())
	yields := 0
	_, err := e.DetailYielding(ctx, "https://example.com/missing", func() error {
		yields++
		cancel()
		return nil
	})

	must.ErrorIs(t, err, context.Canceled)
	must.Eq(t, 1, yields)
}

func testStringColumn(value string) stringColumn {
	return stringColumn{ids: make([]uint32, 32769), values: []string{value}, folded: []string{value}}
}

func facetCount(values []Facet, value string) int {
	for _, facet := range values {
		if facet.Value == value {
			return facet.Rows
		}
	}

	return -1
}
