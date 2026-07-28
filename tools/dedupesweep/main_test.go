package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// testOptions are the defaults the command ships with. Tests that care about a
// threshold override the one field they are about, so a change to a default
// shows up as a failure in the test that depends on it rather than everywhere.
func testOptions() options {
	return options{
		minSharedURL: 1,
		minSharedReq: 5,
		minReqShare:  0.50,
		maxPerKey:    4,
		minReqLen:    4,
		examples:     3,
	}
}

// TestNormaliseURLReachesTheMeasuredFrontEndShapes pins the two relationships
// the audit actually found in 1.29 million postings: Phenom emitting a Workday
// URL with "/apply" appended (Lowe's, KBR, Southwest), and Jibe emitting one
// with an "/en-us" locale segment prepended (FedEx).
func TestNormaliseURLReachesTheMeasuredFrontEndShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    string
		b    string
		same bool
	}{
		{
			name: "phenom apply suffix onto workday, the Lowe's and Southwest shape",
			a:    "https://swa.wd1.myworkdayjobs.com/external/job/Dallas-TX/Flight-Attendant_R-2024-1234/apply",
			b:    "https://swa.wd1.myworkdayjobs.com/external/job/Dallas-TX/Flight-Attendant_R-2024-1234",
			same: true,
		},
		{
			name: "jibe locale prefix onto workday, the FedEx shape",
			a:    "https://fedex.wd1.myworkdayjobs.com/en-us/FXE-EU_External/job/Data-Keying-Agent_RC720040",
			b:    "https://fedex.wd1.myworkdayjobs.com/FXE-EU_External/job/Data-Keying-Agent_RC720040",
			same: true,
		},
		{
			name: "both at once",
			a:    "https://host.example/en_US/job/1/apply",
			b:    "https://HOST.example/job/1",
			same: true,
		},
		{
			name: "query parameter order",
			a:    "https://career8.successfactors.com/career?company=zimmerin01&career_job_req_id=451",
			b:    "https://career8.successfactors.com/career?career_job_req_id=451&company=zimmerin01",
			same: true,
		},
		{
			name: "different requisitions stay different",
			a:    "https://swa.wd1.myworkdayjobs.com/external/job/X/A_R-1/apply",
			b:    "https://swa.wd1.myworkdayjobs.com/external/job/X/A_R-2",
			same: false,
		},
		{
			// docs/dedupe-audit.md: dropping "tracking" parameters merged 10,396
			// URLs, every one of them inside a single board, because a
			// Greenhouse posting's whole identity is its gh_jid. Whatever this
			// normalisation does, it must not do that.
			name: "greenhouse gh_jid is identity, not noise",
			a:    "https://www.mongodb.com/careers/job/?gh_jid=6275509",
			b:    "https://www.mongodb.com/careers/job/?gh_jid=6381035",
			same: false,
		},
		{
			// "careers" is four characters, not two, and must survive.
			name: "a real path segment is not mistaken for a locale",
			a:    "https://example.com/careers/job/1",
			b:    "https://example.com/job/1",
			same: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			a, b := normaliseURL(testCase.a), normaliseURL(testCase.b)

			must.StrNotEqFold(t, "", a, must.Sprint("normalisation returned nothing for", testCase.a))

			if testCase.same {
				test.Eq(t, a, b)
			} else {
				test.NotEq(t, a, b)
			}
		})
	}
}

func TestIsLocaleSegment(t *testing.T) {
	t.Parallel()

	for _, segment := range []string{"en", "us", "en-us", "en_US", "FR-fr"} {
		test.True(t, isLocaleSegment(segment), test.Sprint(segment, "should read as a locale"))
	}

	for _, segment := range []string{"", "job", "jobs", "careers", "e", "en-", "en-usa", "1-2", "en-1"} {
		test.False(t, isLocaleSegment(segment), test.Sprint(segment, "should not read as a locale"))
	}
}

// writeDump writes rows as the NDJSON both this command and dedupeprobe speak.
func writeDump(t *testing.T, rows []row) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "dump.ndjson")

	var out strings.Builder

	encoder := json.NewEncoder(&out)
	for _, item := range rows {
		must.NoError(t, encoder.Encode(item))
	}

	must.NoError(t, os.WriteFile(path, []byte(out.String()), 0o600))

	return path
}

// analyseFile is the command's -in path: read a dump, build the records, and
// analyse them.
func analyseFile(t *testing.T, path string, opts options) []*finding {
	t.Helper()

	opts.in = path

	sources, records, err := analyseDump(opts)
	must.NoError(t, err)

	return analyse(sources, records, opts)
}

// TestSweepFindsTheSouthwestShape rebuilds the Southwest Airlines double count
// in miniature: two sources under DIFFERENT company names, zero URLs matching as
// written, and the Phenom URLs being the Workday URLs with "/apply" appended.
//
// This is the exact case TestNoUnreviewedDoubleCountedEmployer cannot see, and
// the reason this command exists.
func TestSweepFindsTheSouthwestShape(t *testing.T) {
	t.Parallel()

	var rows []row

	for _, req := range []string{"R-1001", "R-1002", "R-1003"} {
		workdayURL := "https://swa.wd1.myworkdayjobs.com/external/job/Dallas/Job_" + req

		rows = append(rows,
			row{Platform: "workday", Key: "https://swa.wd1.myworkdayjobs.com/external", Company: "swa", URL: workdayURL},
			row{Platform: "phenom", Key: "careers.southwestair.com", Company: "southwestair", URL: workdayURL + "/apply"},
		)
	}

	findings := analyseFile(t, writeDump(t, rows), testOptions())

	must.Len(t, 1, findings)

	item := findings[0]

	test.False(t, item.SameName, test.Sprint("the two sides carry different names; that is the blind spot"))
	test.Eq(t, 0, item.Shared[kindURL], test.Sprint("no URL matches as written, which is why Dedupe never collapsed these"))
	test.Eq(t, 3, item.Shared[kindNormURL])
	test.Eq(t, 1.0, item.Strength)
}

// TestSweepFindsTheZimmerShape rebuilds the Zimmer Biomet double count: no URL
// rule reaches it — different path, extra parameters, different order — and only
// the requisition ids match.
func TestSweepFindsTheZimmerShape(t *testing.T) {
	t.Parallel()

	var rows []row

	for _, req := range []string{"451001", "451002", "451003", "451004", "451005", "451006"} {
		rows = append(rows,
			row{
				Platform: "successfactors", Key: "zimmerin01,zimmerin01,career8.successfactors.com",
				Company: "zimmerin01",
				URL:     "https://career8.successfactors.com/career?company=zimmerin01&career_job_req_id=" + req + "&career_ns=job_application",
				ReqID:   req,
			},
			row{
				Platform: "phenom", Key: "careers.zimmerbiomet.com", Company: "zimmerbiomet",
				URL:   "https://career8.successfactors.com/careers?company=zimmerin01&loginFlowRequired=true&career_job_req_id=" + req + "&_s.crb=abc",
				ReqID: req,
			},
		)
	}

	findings := analyseFile(t, writeDump(t, rows), testOptions())

	must.Len(t, 1, findings)

	item := findings[0]

	test.False(t, item.SameName)
	test.Eq(t, 0, item.Shared[kindURL])
	test.Eq(t, 0, item.Shared[kindNormURL], test.Sprint("no URL normalisation reaches this pair; that is the point of it"))
	test.Eq(t, 6, item.Shared[kindReq], test.Sprint("6 of 6 shared on both sides is the Zimmer shape: near-total overlap, which is the only thing a requisition id can prove"))
}

// TestRequisitionThresholdsSuppressCoincidence is the false-positive test, and
// it is the one that decides whether this can run unattended. A requisition id
// is not unique across employers: two unrelated boards sharing a handful of
// short numbers must not redden a report, or the scheduled job gets disabled and
// the blind spot reopens under a green checkmark.
func TestRequisitionThresholdsSuppressCoincidence(t *testing.T) {
	t.Parallel()

	var rows []row

	// Two unrelated 60-posting boards that happen to share four requisition
	// numbers. Below the absolute floor, and 4/60 is below the share floor.
	for i := range 60 {
		rows = append(rows,
			row{Platform: "greenhouse", Key: "alpha", Company: "alpha",
				URL: "https://job-boards.greenhouse.io/alpha/jobs/" + itoa(i), ReqID: "10" + itoa(i)},
			row{Platform: "lever", Key: "beta", Company: "beta",
				URL: "https://jobs.lever.co/beta/" + itoa(i), ReqID: "10" + itoa(i+56)},
		)
	}

	findings := analyseFile(t, writeDump(t, rows), testOptions())

	test.SliceEmpty(t, findings, test.Sprint("four shared requisition numbers between unrelated boards is coincidence, not a finding"))
}

// TestCounterRequisitionsNeedNearTotalOverlap is the regression test for the
// first false positive this sweep produced against live boards.
//
// Its first full run reported `eightfold/fluor` against `jibe/carenewengland` --
// Fluor Corporation and Care New England, unrelated employers -- sharing 136
// requisition ids, 24% of each board, every one of them a four-digit counter
// like "6535". Two dense sequential numbering schemes collide; that is
// arithmetic, not an employer.
//
// The second half of the test is the one that killed a shape-based fix. An
// earlier version held plain numbers to a higher bar than ids carrying a letter,
// on the theory that a letter means entropy. It does not: BrassRing publishes
// 38126BR and Workday publishes R242668, and `brassring/guess` against
// `brassring/publix` shared 116 of those at 31% of each board. The letter is
// constant decoration on a counter, so both halves of this test use the same
// threshold and neither is reported.
func TestCounterRequisitionsNeedNearTotalOverlap(t *testing.T) {
	t.Parallel()

	// The Fluor shape: 560 postings a side, 136 shared bare counters.
	var bare []row

	for i := range 560 {
		bare = append(bare,
			row{Platform: "eightfold", Key: "fluor", Company: "fluor",
				URL: "https://fluor.example/job/" + itoa(i), ReqID: itoa(6000 + i)},
			row{Platform: "jibe", Key: "carenewengland", Company: "carenewengland",
				URL: "https://cne.example/job/" + itoa(i), ReqID: itoa(6424 + i)})
	}

	test.SliceEmpty(t, analyseFile(t, writeDump(t, bare), testOptions()),
		test.Sprint("136 shared four-digit counters between unrelated boards is arithmetic, not an employer"))

	// The Guess/Publix shape: the same overlap wearing a "BR" suffix.
	var decorated []row

	for i := range 560 {
		decorated = append(decorated,
			row{Platform: "brassring", Key: "guess,25813,5079", Company: "guess",
				URL: "https://guess.example/job/" + itoa(i), ReqID: itoa(38000+i) + "BR"},
			row{Platform: "brassring", Key: "publix,26173,5197", Company: "publix",
				URL: "https://publix.example/job/" + itoa(i), ReqID: itoa(38424+i) + "BR"})
	}

	test.SliceEmpty(t, analyseFile(t, writeDump(t, decorated), testOptions()),
		test.Sprint("a letter on a counter is decoration, not identity"))

	// And the shape that is a duplicate: near-total overlap on both sides, which
	// is what Zimmer Biomet and the two UnityPoint gateways both look like.
	var mirrored []row

	for i := range 147 {
		id := itoa(13500+i) + "BR"

		mirrored = append(mirrored,
			row{Platform: "brassring", Key: "unitypoint,25790,5083", Company: "unitypoint",
				URL: "https://sjobs.brassring.com/x?siteid=5083&jobid=" + itoa(i), ReqID: id},
			row{Platform: "brassring", Key: "unitypointmeriter,25790,5084", Company: "unitypointmeriter",
				URL: "https://sjobs.brassring.com/x?siteid=5084&jobid=" + itoa(i), ReqID: id})
	}

	test.SliceNotEmpty(t, analyseFile(t, writeDump(t, mirrored), testOptions()),
		test.Sprint("147 of 147 against 147 of 147, under two different names, is exactly the blind spot"))
}

// TestASmallBoardInsideALargeOneIsNotAPair is the second false positive this
// sweep produced against live boards, and the reason the share is required on
// both sides rather than on the smaller board.
//
// A partial run scored overlap against the smaller board and put seven unrelated
// pairs at the top of its table, all of them small numeric boards against
// `jibe/dunhamssports`: Dunham's Sports publishes 1,610 postings numbered
// densely enough to cover any small board's range, so a 9-posting board matches
// "100%" of itself and 0.6% of Dunham's.
func TestASmallBoardInsideALargeOneIsNotAPair(t *testing.T) {
	t.Parallel()

	var rows []row

	for i := range 1610 {
		rows = append(rows, row{Platform: "jibe", Key: "dunhamssports", Company: "dunhamssports",
			URL: "https://dunhams.example/job/" + itoa(i), ReqID: itoa(100000 + i)})
	}

	for i := range 9 {
		rows = append(rows, row{Platform: "greenhouse", Key: "accela", Company: "accela",
			URL: "https://boards.greenhouse.io/accela/jobs/" + itoa(i), ReqID: itoa(100200 + i)})
	}

	test.SliceEmpty(t, analyseFile(t, writeDump(t, rows), testOptions()),
		test.Sprint("every id of a 9-posting board falling inside a 1,610-posting board's numbering is not an employer"))
}

// TestGenericKeyHeldByManySourcesIsDiscarded covers the other false-positive
// shape: a requisition id like "1000" that half the SMB platform uses. A key
// held by more sources than -max-sources-per-key is a generic string rather than
// an identity, and pairing every holder with every other would generate findings
// quadratically.
func TestGenericKeyHeldByManySourcesIsDiscarded(t *testing.T) {
	t.Parallel()

	var rows []row

	for tenant := range 6 {
		name := "tenant" + itoa(tenant)

		for i := range 20 {
			rows = append(rows, row{
				Platform: "breezy", Key: name, Company: name,
				URL:   "https://" + name + ".breezy.hr/p/" + itoa(i),
				ReqID: "100" + itoa(i),
			})
		}
	}

	findings := analyseFile(t, writeDump(t, rows), testOptions())

	test.SliceEmpty(t, findings, test.Sprint("a requisition number used by six tenants is a generic string"))
}

// TestSameNameOverlapIsSeparatedFromTheBlindSpot checks the split the report
// leads with. A pair whose two sides share a company name is already covered by
// TestNoUnreviewedDoubleCountedEmployer, so it must not be presented as news.
func TestSameNameOverlapIsSeparatedFromTheBlindSpot(t *testing.T) {
	t.Parallel()

	rows := []row{
		{Platform: "ashby", Key: "acme", Company: "acme", URL: "https://acme.example/job/1"},
		{Platform: "greenhouse", Key: "acme", Company: "Acme", URL: "https://acme.example/job/1"},
	}

	findings := analyseFile(t, writeDump(t, rows), testOptions())

	must.Len(t, 1, findings)
	test.True(t, findings[0].SameName, test.Sprint("company names differing only in case are the same name"))
	test.Eq(t, 1, findings[0].Shared[kindURL])
}

// TestOneSourceRepeatingAKeyIsNotAPair guards the intra-board case. A board that
// lists one requisition several times is what internal.Dedupe is for; it says
// nothing about a second route, and counting it would make every Workable board
// look like a duplicate of itself.
func TestOneSourceRepeatingAKeyIsNotAPair(t *testing.T) {
	t.Parallel()

	var rows []row

	for range 10 {
		rows = append(rows, row{
			Platform: "workable", Key: "kreyco", Company: "kreyco",
			URL: "https://apply.workable.com/j/ABC123", ReqID: "REQ-99887",
		})
	}

	findings := analyseFile(t, writeDump(t, rows), testOptions())

	test.SliceEmpty(t, findings)
}

// TestExamplesQuoteTheKeysBehindTheCounts checks the second pass, because a
// count with no example is not actionable and an example drawn from a different
// rule than the count would be worse than none.
func TestExamplesQuoteTheKeysBehindTheCounts(t *testing.T) {
	t.Parallel()

	rows := []row{
		{Platform: "workday", Key: "swa", Company: "swa", URL: "https://swa.example/job/A_R-1"},
		{Platform: "phenom", Key: "southwestair", Company: "southwestair", URL: "https://swa.example/job/A_R-1/apply"},
	}

	path := writeDump(t, rows)
	opts := testOptions()

	findings := analyseFile(t, path, opts)
	must.Len(t, 1, findings)

	must.NoError(t, attachExamples(path, findings, opts))

	examples := findings[0].Examples[kindNames[kindNormURL]]
	must.Len(t, 1, examples)
	test.StrContains(t, examples[0], "https://swa.example/job/A_R-1")
	test.StrNotContains(t, examples[0], "/apply")
}

// TestReportIsRenderedForBothOutcomes exercises the markdown path, including the
// empty case: a sweep that finds nothing must still say what it compared and how
// much of the registry answered, or "no findings" cannot be told apart from "the
// crawl did not run".
func TestReportIsRenderedForBothOutcomes(t *testing.T) {
	t.Parallel()

	opts := testOptions()

	empty := renderReport(
		[]sourceInfo{{Platform: "ashby", Key: "acme", Company: "acme", Postings: 4}},
		nil, nil, opts, 0, false,
	)

	test.StrContains(t, empty, "0 pair(s) with evidence of overlap")
	test.StrContains(t, empty, "southwestair")
	test.StrContains(t, empty, "Examples are unavailable")

	rows := []row{
		{Platform: "workday", Key: "swa", Company: "swa", URL: "https://swa.example/job/A"},
		{Platform: "phenom", Key: "southwestair", Company: "southwestair", URL: "https://swa.example/job/A/apply"},
	}

	path := writeDump(t, rows)
	opts.in = path

	sources, records, err := analyseDump(opts)
	must.NoError(t, err)

	findings := analyse(sources, records, opts)
	report := renderReport(sources, records, findings, opts, 0, true)

	test.StrContains(t, report, "1 pair(s) with evidence of overlap")
	test.StrContains(t, report, "`phenom/southwestair`")
	test.StrContains(t, report, "**new finding**")
}

// TestEmptyBoardsAreReportedAsUnmeasured holds the sweep to the project's rule
// about empty boards: a board that returned nothing cannot be compared with
// anything, and reporting "no overlap" for it would be a claim the run did not
// support.
func TestEmptyBoardsAreReportedAsUnmeasured(t *testing.T) {
	t.Parallel()

	report := renderReport([]sourceInfo{
		{Platform: "ashby", Key: "acme", Company: "acme", Postings: 4},
		{Platform: "lever", Key: "dead", Company: "dead"},
		{Platform: "lever", Key: "broken", Company: "broken", Errors: 3},
	}, nil, nil, testOptions(), 0, true)

	test.StrContains(t, report, "are not evidence of no overlap")
}

func TestCommas(t *testing.T) {
	t.Parallel()

	test.Eq(t, "0", commas(0))
	test.Eq(t, "999", commas(999))
	test.Eq(t, "1,000", commas(1000))
	test.Eq(t, "1,236,756", commas(1236756))
	test.Eq(t, "-1,505", commas(-1505))
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	var digits []byte

	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}
