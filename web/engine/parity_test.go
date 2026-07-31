package engine

// The compiled fast path exists only for speed; its meaning is defined by
// query.Query.Match. This test holds the two together row for row across a
// battery of queries chosen to reach every clause, including the ones that
// only bite on odd inputs: blank terms, unknown enums, the workplace text
// fallback, and mixed-case needles.

import (
	"context"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/query"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/internal/testcorpus"
	"github.com/shoenig/test/must"
)

func TestSearchMatchesQueryMatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	must.NoError(t, testcorpus.Build(ctx, dir, 1))

	e, err := Open(ctx, corpus.DirStore{Dir: dir}, testcorpus.Now)
	must.NoError(t, err)
	must.NoError(t, e.Load(ctx))

	queries := []query.Query{
		{},
		{Remote: true},
		{HasCompensation: true},
		{MinAnnual: 150000},
		{Titles: []string{"ENGINEER"}},
		{Titles: []string{"  engineer  ", ""}},
		{Titles: []string{"", "   "}}, // folds to no constraint
		{ExcludeTitles: []string{"Manager"}},
		{Locations: []string{"remote", "berlin"}},
		{Companies: []string{"ACME"}},
		{Departments: []string{"engineering"}}, // matches department OR team
		{EmploymentTypes: []jobposting.EmploymentType{jobposting.EmploymentTypeContract}},
		{EmploymentTypes: []jobposting.EmploymentType{""}}, // no constraint
		{WorkplaceTypes: []jobposting.WorkplaceType{jobposting.WorkplaceTypeRemote}},
		{WorkplaceTypes: []jobposting.WorkplaceType{jobposting.WorkplaceTypeHybrid}},
		{WorkplaceTypes: []jobposting.WorkplaceType{jobposting.WorkplaceTypeOnsite}},
		{PostedSince: testcorpus.Now.Add(-72 * time.Hour)},
		{
			Titles:          []string{"engineer"},
			ExcludeTitles:   []string{"staff"},
			Locations:       []string{"remote"},
			Remote:          true,
			HasCompensation: true,
			MinAnnual:       100000,
			PostedSince:     testcorpus.Now.Add(-30 * 24 * time.Hour),
		},
	}

	for _, q := range queries {
		c := compileQuery(q)

		for i := range e.rows {
			row := &e.rows[i]
			want := q.Match(&row.posting)
			got := c.match(row)

			if got != want {
				t.Errorf("query %+v, row %d (%q at %q): compiled says %v, query.Match says %v",
					q, i, row.posting.Title, row.posting.Company, got, want)
			}
		}
	}
}
