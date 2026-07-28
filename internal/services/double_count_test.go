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

	// sameEmployerDisjointBoards means one employer publishes different work on
	// each platform, so nothing is counted twice and both boards are worth
	// keeping.
	//
	// This turned out to be the common shape for large employers, and it is the
	// reason this list cannot be a prohibition. Home Depot serves 22,899 hourly
	// and store roles through BrassRing and 972 corporate roles through Workday,
	// with zero shared URLs and zero shared titles. Bechtel serves craft trades
	// through BrassRing (Carpenter, Hydraulic Crane Operator, Material Person)
	// and professional roles through Phenom (Field Engineer, Prime Contracts
	// Manager). Deleting either route in the name of avoiding a duplicate would
	// have thrown away the half of the employer this project covers least well,
	// which is exactly the hourly and skilled-trade work docs/source-backlog.md
	// says the registry under-represents.
	sameEmployerDisjointBoards

	// sameEmployerPartialOverlap means one employer's two boards share some
	// postings and not others, so neither deleting a route nor keeping both is
	// free.
	//
	// Both are kept, because the arithmetic favours it: AT&T publishes 2,171
	// postings on Radancy and 1,271 on Workday sharing 366 title+location pairs,
	// so deleting the smaller route removes about 900 postings that exist
	// nowhere else in order to stop counting 366 twice. The overlap is recorded
	// as a number rather than a judgement so that it can be watched: a pair
	// drifting towards total overlap is a route that has become a mirror and
	// should be re-decided.
	sameEmployerPartialOverlap

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
// Re-measuring those 7 the same day cut them to 6. Visa was on the list because
// its two boards shared "2 of 2 titles", and the two titles were the bare words
// "Sr. Manager" and "Director"; the reqs behind them exist on neither of the
// other board. Titles are a weak signal even after the name test passes, which
// is why the paragraph below asks for URLs first.
//
// # What a maintainer does with this
//
// Adding a source that collides with an existing one fails this test. Resolve it
// by measuring, not by guessing: crawl both and compare.
//
// **Compare URLs first, then titles.** [internal.Dedupe] keys on URL, so two
// sources returning the same URLs are already collapsed and cost nothing but a
// request. A count is inflated only when the same opening arrives under two
// different URLs, which is what shared titles and disjoint URLs together mean.
// Titles alone are a weaker signal in both directions: one employer's two boards
// can carry entirely different job families and share no title at all while
// genuinely duplicating nothing, and two boards can share a title like "Software
// Engineer" while belonging to unrelated companies.
//
// Only when both boards serve the same openings is a route worth deleting.
// Prefer keeping the one that returns more of the board, and where they tie, the
// one that costs fewer requests per posting
// (docs/measurements/2026-07-28-crawl.md ranks the platforms).
//
// The sameEmployer rows this list used to carry are gone, because the routes
// they described are gone: see [deletedDoubleCountRoutes], which is where a
// resolved overlap lands and what stops the deleted route coming back.
//
// # What this map cannot see
//
// It is keyed on company name, so it only catches an employer whose two routes
// are named the same. A pair named differently on each side is invisible to it,
// and that is not hypothetical: the Phenom/Workday duplicate for Southwest
// Airlines was registered as "southwestair" against "swa", and the
// Phenom/SuccessFactors one for Zimmer Biomet as "zimmerbiomet" against
// "zimmerin01". Both were real -- 15 and 362 postings counted twice -- and both
// were found by a URL audit rather than by this test, which stayed green
// throughout. They are recorded in [deletedDoubleCountRoutes].
//
// Closing that gap properly means comparing boards rather than names, which is
// a crawl and not a unit test. docs/dedupe-audit.md is the periodic sweep that
// does it, and tools/dedupeprobe is what runs it. This map remains worth having
// for the case it does catch: a name collision is the cheap half of the problem
// and the half a new registration is most likely to introduce.
var reviewedDoubleCounts = map[string]struct {
	verdict doubleCountVerdict
	note    string
}{
	// No sameEmployer rows remain. The six that existed on 2026-07-28 were
	// re-measured and resolved by deleting a route; they are recorded in
	// [deletedDoubleCountRoutes].

	// Unrelated companies that share a name. Both belong here.
	"extend":    {differentEmployers, "ashby 9, greenhouse 18, no shared title"},
	"justworks": {differentEmployers, "greenhouse 98, smartrecruiters 7, no shared title"},
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

	"deepmind": {sameEmployerDisjointBoards, "direct 89 postings from deepmind.google, greenhouse 10, " +
		"zero shared URLs and zero shared title+location pairs. The Alphabet adapter landed in #34 and the " +
		"greenhouse board is a small separate listing"},
	"netflix": {oneSided, "eightfold 476, lever returned none. #35 recorded the same thing when it added " +
		"the eightfold tenant: the lever slug is stale rather than duplicated, and is left registered because " +
		"an empty board is not evidence of a dead one"},

	// Measured 2026-07-28 by the URL sweep in docs/dedupe-audit.md, which is why
	// this is no longer `unmeasured`. The two routes are structurally the same
	// board -- jibe emits .../en-us/FXE-EU_External/job/.../apply where workday
	// emits .../FXE-EU_External/job/... -- but content overlap is ONE posting,
	// because Workday's own API reports total 337 against jibe's 4,933 view of
	// the same site, and Workday answers 403 for a requisition jibe still
	// advertises (RC720040) while answering 200 for one it lists (RC772413-1).
	//
	// Both kept, but not comfortably: the honest reading is that jibe's index is
	// stale rather than richer, so ~4,600 of those postings may be delisted reqs
	// this project is still publishing. That is a data-quality question about one
	// adapter's freshness, not a double count, and it wants its own measurement
	// rather than a deletion decided here.

	// One employer, two boards carrying different work. Both kept.
	"homedepot": {sameEmployerDisjointBoards, "brassring 22,899 hourly and store roles, workday 972 corporate; " +
		"zero shared URLs and zero shared titles. BrassRing is the larger board by 23x and is the half " +
		"this registry covered not at all"},
	"bechtel": {sameEmployerDisjointBoards, "brassring 433 craft trades (Carpenter, Hydraulic Crane Operator), " +
		"phenom 1,018 professional roles (Field Engineer, Prime Contracts Manager); no shared title"},
	"unitypoint": {sameEmployerDisjointBoards, "brassring 282, jibe 1,362, zero shared URLs and zero shared titles"},
	"walgreens": {sameEmployerDisjointBoards, "brassring 5,500 and radancy 10,000 (of 21,232, capped by " +
		"radancyMaxWindow). Zero shared URLs and zero shared title+location pairs across 4,397 and 7,593 " +
		"distinct pairs -- the cleanest split measured: two systems carrying entirely different Walgreens work"},
	"sanofi": {sameEmployerDisjointBoards, "radancy 1,015, workday 1,067, zero shared URLs and zero shared " +
		"title+location pairs across 977 and 1,021. Near-identical board sizes with nothing in common"},
	"wegmans": {sameEmployerDisjointBoards, "radancy 499, workday 481, zero shared URLs and zero shared " +
		"title+location pairs across 476 and 479"},
	"veolia": {sameEmployerDisjointBoards, "radancy 2,962, successfactors 91, zero shared URLs and zero " +
		"shared title+location pairs"},
	"carnival": {sameEmployerDisjointBoards, "oraclecloud 229, radancy 128, zero shared URLs, 14 of radancy's " +
		"124 title+location pairs shared -- 11% of the smaller board, and both carry work the other does not"},

	// One employer, two boards that partly mirror each other. Both kept, and the
	// overlap is recorded as a number so it can be watched.
	"att": {sameEmployerPartialOverlap, "radancy 2,171, workday 1,271, zero shared URLs, 366 of workday's " +
		"1,108 title+location pairs shared (33%). Deleting workday would drop ~900 postings that exist " +
		"nowhere else to stop counting 366 twice"},
	"citi": {sameEmployerPartialOverlap, "radancy 3,484, workday 2,000, zero shared URLs, 825 of workday's " +
		"1,900 title+location pairs shared (43%) -- the highest overlap that is still not a mirror; " +
		"workday keeps ~1,075 pairs radancy does not carry"},
	"disney": {sameEmployerPartialOverlap, "radancy 798, workday 668, zero shared URLs, 161 of workday's " +
		"628 title+location pairs shared (26%)"},
	"chipotle": {sameEmployerDisjointBoards, "radancy 7,659 restaurant roles on jobs.chipotle.com, workday 181 on " +
		"chipotle.wd5. Zero shared URLs, and the 52-of-55 shared TITLES are the trap this file warns about: " +
		"compared as title+location, only 12 of workday's 178 distinct pairs appear in radancy, because " +
		"'General Manager' recurs at thousands of restaurants. 166 workday postings exist nowhere else, and " +
		"radancy carries 7,600 workday does not, so the 12 overlaps are 0.15% of the pair and deleting either " +
		"route would lose thousands of real postings to save twelve duplicates"},

	// Re-measured 2026-07-28 and reclassified out of sameEmployer. The earlier
	// row read "2 of 2 titles shared" and concluded smartrecruiters was a
	// remnant to delete. It is the same employer -- the smartrecruiters tenant
	// sets a custom field "Visa Inc. or Visa in Europe job?" to "Visa Inc." --
	// but the two titles it shares with Workday are the bare words "Sr. Manager"
	// and "Director", which is exactly the weak signal the header above warns
	// about. Workday holds four postings with those titles and none of them is
	// either of these: the smartrecruiters reqs are REF97395W and REF97388Z and
	// neither appears anywhere in Workday's 880, whose reqs are all six-digit
	// (REF0xxxxxW); the smartrecruiters "Sr. Manager" is in Austin, TX and
	// Workday has no Austin posting by that title. So nothing is counted twice
	// and deleting smartrecruiters would have removed two postings on a title
	// coincidence.
	//
	// Recorded honestly, those two postings are close to worthless: both have an
	// empty company description, job description and qualifications, and a
	// job-family label where a title should be. They look like residue of a
	// migration to Workday. But "low value" is not "duplicated", the whole
	// tenant costs about two seconds a crawl, and this list is not the place to
	// make a coverage cut on a different argument than the one it measures.
	"visa": {sameEmployerDisjointBoards, "smartrecruiters 2, workday 880, zero shared URLs. " +
		"The 2 shared titles are the generic words Sr. Manager and Director; the smartrecruiters " +
		"reqs REF97395W and REF97388Z are absent from all 880 workday postings, so the boards are " +
		"disjoint and the earlier sameEmployer verdict was a false positive from one-word titles"},

	// Short names that collide on the SMB platform. Breezy tenants are small and
	// none of these is the well-known holder of the name.
	"adobe":     {differentEmployers, "breezy 1 posting, workday 834, no shared title; the breezy tenant is not Adobe"},
	"alasco":    {differentEmployers, "breezy 12, personio 11, no shared title"},
	"brilliant": {differentEmployers, "breezy 2, lever 4, no shared title"},
	"duolingo":  {differentEmployers, "breezy 3, greenhouse 59, no shared title"},
	"framework": {differentEmployers, "breezy 10, rippling 1, no shared title"},
}

// deletedDoubleCountRoutes records a platform+key that was registered, measured
// against another route for the same employer, found to serve the same openings
// under different URLs, and deleted.
//
// It exists because [reviewedDoubleCounts] cannot hold this. A resolved overlap
// stops being an overlap the moment the losing route is deleted, so
// TestReviewedDoubleCountsHasNoStaleRows requires its row to go, and it is right
// to: a row left behind would pre-approve re-adding the very route that was
// deleted. But then the measurement that justified the deletion is nowhere, and
// the next person to sweep a platform for missing tenants re-adds
// greenhouse/amplitude in good faith and silently restores the double count. So
// the evidence moves here, keyed by the exact route rather than the company
// name, and TestDeletedDoubleCountRoutesStayDeleted turns re-adding one back
// into a failure with the reason attached.
//
// Keys are "platform/key", matching [Source.Platform] and [Source.Key].
//
// # On the three Ashby-versus-Greenhouse ties
//
// All three tied on posting count, and docs/measurements/2026-07-28-crawl.md
// ranks Greenhouse cheaper: 4.6 s per 1,000 postings against Ashby's 5.9. Ashby
// was kept anyway, because a tie in postings is not a tie in what a posting
// carries. Greenhouse deliberately fetches the list response without
// "?content=true" -- see the note on [greenhouseJobs], a platform-wide decision
// about a 13.7x response-size increase, and the right one -- so its postings
// arrive with no department, no employment type, no remote flag and no pay.
// Measured across these three boards, Ashby returned all four fields on all 267
// postings, and 36 compensation ranges Greenhouse returned none of. The cost the
// cheaper-platform tie-break would have saved is 1.3 s per 1,000 postings, which
// over 267 postings is about a third of a second.
var deletedDoubleCountRoutes = map[string]string{
	"jibe/fedex": "measured 2026-07-28 and deleted as a dead board, not as a duplicate. It advertised " +
		"138,214 postings against 693 live requisitions across all 23 of FedEx's own Workday sites, and " +
		"it names itself: ats_code is \"fedex-prod-historical-jobs-feed\" on 2,400 of 2,400 sampled " +
		"postings. Every bucket that can be checked is delisted -- 320 of 320 sampled Workday " +
		"requisitions answer 403 from FedEx's own CXS API (method validated: 12 of 12 taken off the live " +
		"board answer 200, and a made-up one answers 404, so 403 is not a path artefact), all five " +
		"BrassRing gateways report JobsCount 0, and 15 of 15 sampled Taleo apply URLs answer 404. The " +
		"remaining 17.1% point at Paradox and cannot be checked without a browser. FedEx stays covered " +
		"through its two registered Workday sites. See testdata/jibe_fedex_freshness.tsv",
	"phenom/careers.southwestair.com": "measured 2026-07-28: phenom 18 postings, workday swa.wd1 43, zero raw " +
		"URL overlap but 15 of the 18 phenom URLs are exactly a workday URL plus \"/apply\". Kept workday, " +
		"which returns the larger board. Invisible to reviewedDoubleCounts because that map keys on company " +
		"name and these two sides are named southwestair and swa",
	"phenom/careers.zimmerbiomet.com": "measured 2026-07-28: phenom 376, successfactors zimmerin01 373, " +
		"362 of 365 career_job_req_id values shared. No URL rule reaches this one -- phenom published " +
		"career8.successfactors.com/careers?...loginFlowRequired&career_os&_s.crb=... where the " +
		"successfactors adapter publishes /career?...career_job_req_id&career_ns: different path, extra " +
		"params, different order. Kept successfactors, the employer's own ATS. Also invisible to " +
		"reviewedDoubleCounts, which keys on name: zimmerbiomet against zimmerin01",
	"phenom/careers.kbr.com": "measured 2026-07-28: 1,556 of careers.kbr.com's 1,558 distinct URLs are " +
		"exactly a registered kbr.wd5.myworkdayjobs.com URL with \"/apply\" appended, and zero match one as " +
		"written, so internal.Dedupe never collapsed them. Same shape as Lowe's: the Phenom site is a front " +
		"end onto the Workday tenant this project already crawls. Kept workday. This row also corrects an " +
		"earlier verdict of differentEmployers for \"kbr\", which was measured with `--company kbr` -- " +
		"substring matching, so it compared two unrelated tenants and never looked at phenom or workday at all",
	"greenhouse/amplitude": "measured 2026-07-28: ashby 46 postings, greenhouse 46, 41 of 41 titles " +
		"shared, zero shared URLs. Kept ashby: equal coverage, and ashby carried department, " +
		"employment type and remote on all 46 plus pay on 34 where greenhouse carried none",
	"greenhouse/clickhouse": "measured 2026-07-28: ashby 173, greenhouse 173, 81 of 81 titles shared, " +
		"zero shared URLs. Kept ashby: equal coverage, and ashby carried department, employment type " +
		"and remote on all 173 where greenhouse carried none",
	"greenhouse/fireworksai": "measured 2026-07-28: ashby 48, greenhouse 48, 48 of 48 titles shared, " +
		"zero shared URLs. Kept ashby: equal coverage, and ashby carried department, employment type " +
		"and remote on all 48 plus pay on 2 where greenhouse carried none",
	"lever/qonto": "measured 2026-07-28: ashby 43, lever 43, 42 of 42 titles shared, zero shared URLs. " +
		"Kept ashby: equal coverage, cheaper (5.9 s/1k against 8.2), and it published employment type " +
		"on all 43 against lever's 39",
	"ashby/secureframe": "measured 2026-07-28: ashby 18, lever 20, all 18 ashby titles shared, zero " +
		"shared URLs. Kept lever: it returns the whole board and ashby a subset, missing Growth " +
		"Account Executive and Growth Marketing Manager. Ashby carries department and employment type " +
		"that lever does not, but a field on a posting the other route never returns is worth less " +
		"than the posting",
	"phenom/talent.lowes.com": "measured 2026-07-28: phenom 5,103 postings over 4,731 distinct URLs, " +
		"workday lowes.wd5.myworkdayjobs.com/LWS_External_CS 11,283. Literally zero URLs matched, " +
		"which is why internal.Dedupe never collapsed them, and the reason is not that the boards " +
		"differ: this Phenom site is a front end onto that same Workday tenant and its links are the " +
		"Workday posting URL with '/apply' appended, because the adapter yields Phenom's applyUrl. " +
		"Strip that suffix and 4,729 of 4,731 phenom URLs are workday URLs; the 2 that are not are " +
		"workday reqs (JR-02561381, JR-02392060) delisted between the two probes. All 428 phenom " +
		"titles are among workday's 495 and phenom carried nothing workday did not. This was the " +
		"largest single double count in the registry",
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

// TestDeletedDoubleCountRoutesStayDeleted fails when a route that was measured
// and deleted as a duplicate is registered again.
//
// This is the half of the guard [TestNoUnreviewedDoubleCountedEmployer] cannot
// give. That test fires on a *name* appearing on two platforms, so it catches a
// re-add only while some other route still holds the name -- and for
// phenom/talent.lowes.com it would, but it would be satisfied by any row in
// [reviewedDoubleCounts], including one a future maintainer adds in good faith
// after re-measuring badly. Keying on the exact platform+key instead means the
// specific route that was already shown to be redundant cannot come back
// quietly, and the failure carries the measurement rather than asking for a new
// one.
func TestDeletedDoubleCountRoutesStayDeleted(t *testing.T) {
	t.Parallel()

	for _, source := range Builtin {
		route := source.Platform + "/" + source.Key

		note, deleted := deletedDoubleCountRoutes[route]

		test.False(t, deleted, test.Sprintf(
			"%s is registered again, but it was deleted as a measured duplicate: %s. "+
				"If the boards have since diverged, re-measure both and say so here; "+
				"do not restore it because a platform sweep found the slug.",
			route, note))
	}
}

// TestEveryDeletedDoubleCountRouteCarriesItsEvidence holds the deleted list to
// the same standard as [reviewedDoubleCounts]: a route recorded as redundant
// with no measurement behind it is an assertion nobody can check.
func TestEveryDeletedDoubleCountRouteCarriesItsEvidence(t *testing.T) {
	t.Parallel()

	for route, note := range deletedDoubleCountRoutes {
		must.StrNotEqFold(t, "", strings.TrimSpace(note),
			must.Sprintf("%q is recorded as deleted but records no evidence for it", route))

		platform, key, ok := strings.Cut(route, "/")

		must.True(t, ok && platform != "" && key != "",
			must.Sprintf("%q is not in platform/key form, so no registration can match it", route))
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

// TestNoOverlapIsLeftRecordedAsAnUnresolvedDuplicate asserts that no row is
// sitting at [sameEmployer].
//
// That verdict means both boards serve the same openings, which is the one case
// where a route should be deleted rather than recorded. Leaving the row instead
// would park a known double count in the trend line indefinitely with a note
// explaining it, which is worse than either fixing it or not noticing it: the
// number stays wrong and the wrongness looks reviewed.
//
// So [sameEmployer] is a transient state a maintainer passes through while
// resolving an overlap, not a resting place. The resolution lands in
// [deletedDoubleCountRoutes], and this is what stops it being skipped.
func TestNoOverlapIsLeftRecordedAsAnUnresolvedDuplicate(t *testing.T) {
	t.Parallel()

	for name, reviewed := range reviewedDoubleCounts {
		test.NotEq(t, sameEmployer, reviewed.verdict, test.Sprintf(
			"%q is recorded as sameEmployer, which means both boards serve the same openings. "+
				"Delete the weaker route and record it in deletedDoubleCountRoutes with the "+
				"measurement, rather than leaving a known double count in the registry: %s",
			name, reviewed.note))
	}
}
