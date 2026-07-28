package services

import (
	"sort"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// doubleCountVerdict is what a live comparison of two boards concluded about a
// company name that appears on more than one platform.
type doubleCountVerdict int

const (
	// sameEmployer means both boards serve the same openings. The postings are
	// counted twice: [internal.Dedupe] keys on URL and the same req has a
	// different URL on each route, so nothing downstream collapses them.
	sameEmployer doubleCountVerdict = iota

	// differentEmployers means two unrelated companies happen to share a short
	// name. Nothing is double counted and both belong in the registry.
	differentEmployers

	// oneSided means only one of the two boards returned postings when this was
	// measured. It is not evidence either way: an empty board belongs to a
	// company that is not hiring today as often as to a dead slug, which
	// docs/adding-a-source.md is explicit about.
	oneSided

	// unmeasured means the comparison has not been run. New entries start here.
	unmeasured
)

// reviewedDoubleCounts records every company name registered on more than one
// platform, with what a live comparison found.
//
// # Why this exists, and why it is a list rather than a prohibition
//
// A company crawled on two platforms contributes its postings twice, and
// jobs_record.txt is a trend line where a step change that reflects no hiring is
// indistinguishable from one that does. So a new overlap must not appear
// unnoticed.
//
// But "this name is on two platforms" is not the same claim as "this employer is
// crawled twice", and treating them alike would force wrong deletions. Measured
// live on 2026-07-28 by fetching both boards and comparing their distinct
// posting titles, of 20 name overlaps only 7 were the same employer; 8 were
// unrelated companies sharing a short word, and 5 had one side publishing
// nothing at the time. Deleting on the name alone would have removed eight real
// employers from the registry.
//
// # What a maintainer does with this
//
// Adding a source that collides with an existing one fails this test. Resolve it
// by measuring, not by guessing: crawl both, compare the titles, and add a row
// here with what you found. Prefer keeping the route that returns more of the
// board, and where they tie, the one that costs fewer requests per posting
// (docs/measurements/2026-07-28-crawl.md ranks the platforms).
//
// The sameEmployer rows are a known, quantified defect rather than an accepted
// one. Resolving each means deleting one route, which is a coverage decision
// with a real cost, so they are recorded with the evidence needed to make it
// rather than resolved unilaterally here.
var reviewedDoubleCounts = map[string]struct {
	verdict doubleCountVerdict
	note    string
}{
	// Same employer. Postings counted twice today.
	"amplitude":   {sameEmployer, "ashby 46 postings, greenhouse 46, 41 of 41 distinct titles shared; keep one"},
	"clickhouse":  {sameEmployer, "ashby 173, greenhouse 173, 81 of 81 titles shared; keep one"},
	"fireworksai": {sameEmployer, "ashby 48, greenhouse 48, 48 of 48 titles shared; keep one"},
	"lowes": {sameEmployer, "phenom 4,814, workday 11,284, 428 of 428 titles shared. " +
		"The largest overlap in the registry: Workday returns more than twice the board, " +
		"so Workday is the route to keep, and roughly 4,800 postings are counted twice until it is"},
	"qonto":       {sameEmployer, "ashby 43, lever 43, 42 of 42 titles shared; keep one"},
	"secureframe": {sameEmployer, "ashby 18, lever 20, 18 of 18 titles shared; keep one"},
	"visa":        {sameEmployer, "smartrecruiters 2, workday 878, 2 of 2 titles shared; workday is the board, smartrecruiters is a remnant"},

	// Unrelated companies that share a name. Both belong here.
	"extend":    {differentEmployers, "ashby 9, greenhouse 18, no shared title"},
	"justworks": {differentEmployers, "greenhouse 98, smartrecruiters 7, no shared title"},
	"kbr":       {differentEmployers, "oraclecloud 2, personio 7, no shared title"},
	"ledger":    {differentEmployers, "ashby 10, lever 1, no shared title"},
	"radar":     {differentEmployers, "ashby 18, greenhouse 17, no shared title"},
	"reach":     {differentEmployers, "ashby 3, greenhouse 5, no shared title"},
	"warp":      {differentEmployers, "ashby 18, greenhouse 2, no shared title"},
	"watershed": {differentEmployers, "ashby 38, greenhouse 8, no shared title"},

	// One side published nothing when measured, which settles nothing.
	"dnb":              {oneSided, "lever 133, workday returned none"},
	"epicgames":        {oneSided, "greenhouse 142, workday returned none"},
	"openly":           {oneSided, "ashby 11, greenhouse returned none"},
	"plaid":            {oneSided, "ashby 115, lever returned none"},
	"protectdemocracy": {oneSided, "recruitee 13, bamboohr returned none"},

	"fedex": {unmeasured, "jibe and workday; the comparison timed out and has not been repeated"},
}

// companiesOnMoreThanOnePlatform groups the registry by company name.
func companiesOnMoreThanOnePlatform() map[string][]string {
	platforms := make(map[string]map[string]bool)

	for _, source := range Builtin {
		name := strings.ToLower(source.Company)

		if platforms[name] == nil {
			platforms[name] = make(map[string]bool)
		}

		platforms[name][source.Platform] = true
	}

	overlaps := make(map[string][]string)

	for name, set := range platforms {
		if len(set) < 2 {
			continue
		}

		names := make([]string, 0, len(set))
		for platform := range set {
			names = append(names, platform)
		}

		sort.Strings(names)
		overlaps[name] = names
	}

	return overlaps
}

// TestNoUnreviewedDoubleCountedEmployer fails when a company appears on a
// platform it was not already known to appear on.
//
// This replaces two per-platform copies of the same idea that lived in
// successfactors_test.go and oracle_orc_test.go. Those enforced on 2 of 19
// platforms a rule the other 17 visibly broke -- 21 overlaps existed in the
// registry while they passed -- so they fired only for whichever platform was
// registered last, and the first thing they caught after Oracle Cloud landed was
// an unrelated employer with a similar name rather than a real duplicate.
//
// Deriving the overlap set from [Builtin] rather than checking one platform
// against the rest is what makes it cover every platform at once, and is the
// same correction the limiter coverage test needed: a check that enumerates its
// own subjects passes vacuously for anything added later.
func TestNoUnreviewedDoubleCountedEmployer(t *testing.T) {
	t.Parallel()

	for name, platforms := range companiesOnMoreThanOnePlatform() {
		reviewed, ok := reviewedDoubleCounts[name]

		test.True(t, ok, test.Sprintf(
			"%q is now registered on %s and no comparison of those boards is recorded. "+
				"Crawl both, compare their posting titles, and add a row to reviewedDoubleCounts "+
				"saying whether they are the same employer counted twice or two companies "+
				"sharing a name. Do not resolve it by guessing from the name.",
			name, strings.Join(platforms, " and ")))

		if ok && reviewed.verdict == unmeasured {
			t.Logf("%s: overlap on %s is recorded but never measured (%s)",
				name, strings.Join(platforms, " and "), reviewed.note)
		}
	}
}

// TestReviewedDoubleCountsHasNoStaleRows keeps the list honest in the other
// direction. A row for a company that no longer collides is a claim about the
// registry that stopped being true, and left in place it would quietly permit a
// future re-registration that nobody reviewed.
func TestReviewedDoubleCountsHasNoStaleRows(t *testing.T) {
	t.Parallel()

	overlaps := companiesOnMoreThanOnePlatform()

	for name := range reviewedDoubleCounts {
		_, ok := overlaps[name]

		test.True(t, ok, test.Sprintf(
			"%q is recorded in reviewedDoubleCounts but is no longer registered on more "+
				"than one platform. Delete the row: leaving it would silently pre-approve "+
				"re-adding the duplicate.", name))
	}
}

// TestEveryReviewedDoubleCountCarriesItsEvidence stops a row from being added
// with a verdict and no measurement behind it, which is how a list like this
// decays into a list of exemptions.
func TestEveryReviewedDoubleCountCarriesItsEvidence(t *testing.T) {
	t.Parallel()

	for name, reviewed := range reviewedDoubleCounts {
		must.StrNotEqFold(t, "", strings.TrimSpace(reviewed.note),
			must.Sprintf("%q has a verdict but records no evidence for it", name))
	}
}
