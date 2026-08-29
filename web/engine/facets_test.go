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
	row := record{
		posting: jobposting.JobPosting{
			EmploymentType: "unexpected-employment-value",
			WorkplaceType:  "unexpected-workplace-value",
			PostedAt:       now.Add(time.Hour),
		},
		firstSeen: now.Add(time.Hour),
	}

	facets := newFacets()
	facets.add(&row, now)

	must.Eq(t, 1, facetCount(facets.Employment, "unknown"))
	must.Eq(t, 1, facetCount(facets.Workplace, "unknown"))
	must.Eq(t, 1, facetCount(facets.PostedAge, "unknown"))
	must.Eq(t, 1, facetCount(facets.FirstSeenAge, "unknown"))
}

func TestFacetCountingHasConstantMemory(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	row := record{
		posting: jobposting.JobPosting{
			EmploymentType: jobposting.EmploymentTypeFullTime,
			WorkplaceType:  jobposting.WorkplaceTypeRemote,
			PostedAt:       now,
		},
		firstSeen: now,
	}
	facets := newFacets()

	allocations := testing.AllocsPerRun(1000, func() {
		facets.add(&row, now)
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
		rows:  make([]record, rows),
		order: make([]int, rows),
	}
	for i := range e.order {
		e.order[i] = i
	}

	ctx, cancel := context.WithCancel(t.Context())
	yields := 0
	_, err := e.SearchYielding(ctx, SearchRequest{}, func() error {
		yields++
		cancel()
		return nil
	})

	must.ErrorIs(t, err, context.Canceled)
	must.Eq(t, 1, yields)
}

func facetCount(values []Facet, value string) int {
	for _, facet := range values {
		if facet.Value == value {
			return facet.Rows
		}
	}

	return -1
}
