package companies

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	jobpostings "github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestGoogle(t *testing.T) {
	t.Parallel()
	tests.RequireNetwork(t)

	var found int

	for jobPosting, err := range GoogleBoard(deepmindCompany, "DeepMind")(t.Context(), httpx.NewClient()) {
		must.NoError(t, err)
		tests.CheckJobPosting(t, jobPosting)

		found++
	}

	t.Logf("found %d job postings for DeepMind", found)
}

// googlePage renders one search page the way the careers site serves it: a
// server-rendered document with the results embedded in an AF_initDataCallback
// argument, wrapped in enough surrounding noise that a parser cannot succeed by
// assuming the payload starts at a fixed offset.
//
// hash varies between the site's deploys, so it is varied here too.
func googleFixturePage(total int, hash string, records ...string) string {
	jobs := "null"
	if len(records) > 0 {
		jobs = "[" + strings.Join(records, ",") + "]"
	}

	return `<!doctype html><html><head><title>Search jobs</title>` +
		`<script nonce="x">AF_initDataCallback({key: 'ds:0', hash: '1', data:[[["projects/gweb-careers-proto/tenants/t/companies/c","Google","google"]]], sideChannel: {}});</script>` +
		`<script nonce="x">AF_initDataCallback({key: 'ds:1', hash: '` + hash + `', data:[` +
		jobs + `,null,` + fmt.Sprint(total) + `,20], sideChannel: {}});</script>` +
		`</head><body><div>data: not the payload</div></body></html>`
}

// googleRecordJSON builds one positional posting record.
//
// The field order mirrors the live payload exactly, because that order is the
// thing under test: this adapter reads by index, and a fixture that invented its
// own order would verify nothing.
func googleRecordJSON(id, title, applyURL, company, locations, description string, seconds int64) string {
	apply := "null"
	if applyURL != "" {
		apply = `"` + applyURL + `"`
	}

	return `[` +
		`"` + id + `",` + // 0 id
		`"` + title + `",` + // 1 title
		apply + `,` + // 2 apply URL
		`[null,"<ul><li>Do the work.</li></ul>"],` + // 3 responsibilities
		`[null,"<h3>Minimum qualifications:</h3>"],` + // 4 qualifications
		`"projects/gweb-careers-proto/tenants/t/companies/c",` + // 5 resource path
		`null,` + // 6
		`"` + company + `",` + // 7 company
		`"en-US",` + // 8 locale
		locations + `,` + // 9 locations
		`[null,"` + description + `"],` + // 10 description
		`[2],` + // 11 level
		`[` + fmt.Sprint(seconds) + `,223000000],` + // 12 timestamp
		`[` + fmt.Sprint(seconds) + `,223000000],` + // 13 timestamp
		`[` + fmt.Sprint(seconds+2) + `,257000000],` + // 14 timestamp
		`[null,"Notes."],null,null,[null,""],[null,"<ul></ul>"],2]` // 15-20
}

const (
	// googleUSLocation and googleCALocation are location entries in the live
	// shape: display name, address lines, city, postal code, region, country.
	googleUSLocation = `[["San Jose, CA, USA",["San Jose, CA, USA"],"San Jose",null,"CA","US"]]`
	googleCALocation = `[["Waterloo, ON, Canada",["Waterloo, ON, Canada"],"Waterloo",null,"ON","CA"]]`

	// googleBothLocations is a posting open in both countries, which is the
	// case a two-range pay block cannot be resolved against.
	googleBothLocations = `[["Waterloo, ON, Canada",["Waterloo, ON, Canada"],"Waterloo",null,"ON","CA"],` +
		`["San Jose, CA, USA",["San Jose, CA, USA"],"San Jose",null,"CA","US"]]`
)

// googleStub serves fixture pages by page number.
//
// It fails after maxRequests so an adapter that has lost its pagination bound
// fails in milliseconds and says why, rather than running until the test
// binary's timeout.
type googleStub struct {
	pages       map[int]string
	maxRequests int
	requests    int
}

func (g *googleStub) RoundTrip(req *http.Request) (*http.Response, error) {
	g.requests++

	if g.requests > g.maxRequests {
		return nil, fmt.Errorf("made %d requests against a search that never ends: pagination is unbounded", g.requests)
	}

	page := 1
	if raw := req.URL.Query().Get("page"); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &page); err != nil {
			return nil, fmt.Errorf("stub could not read the page parameter %q: %w", raw, err)
		}
	}

	body, served := g.pages[page]
	if !served {
		// Past the end of the fixture board, which the site answers with a
		// payload whose job list is null.
		body = googleFixturePage(len(g.pages)*20, "9")
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func googleDrain(t *testing.T, stub *googleStub) ([]*jobpostings.JobPosting, []error) {
	t.Helper()

	client := &http.Client{Transport: stub}

	var (
		postings []*jobpostings.JobPosting
		errs     []error
	)

	for posting, err := range GoogleBoard(googleCompany, "Google")(t.Context(), client) {
		if err != nil {
			errs = append(errs, err)
			continue
		}

		postings = append(postings, posting)
	}

	return postings, errs
}

func TestGoogleReadsAPosting(t *testing.T) {
	t.Parallel()

	stub := &googleStub{maxRequests: 5, pages: map[int]string{
		1: googleFixturePage(1, "2", googleRecordJSON(
			"94266795891794630", "Staff Software Engineer",
			"https://www.google.com/about/careers/applications/signin?jobId=abc",
			"Google", googleUSLocation,
			"<p>Build things.</p>", 1785148642,
		)),
	}}

	postings, errs := googleDrain(t, stub)

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	posting := postings[0]

	test.Eq(t, "google", posting.Company)
	test.Eq(t, "Staff Software Engineer", posting.Title)
	test.Eq(t, "San Jose, CA, USA", posting.Location)
	test.Eq(t, "94266795891794630", posting.ExternalID)

	// Built from the id alone, not from the apply link and not from a title
	// slug: the apply link is a sign-in redirect carrying a session token, and a
	// slug would change whenever the title was reworded, splitting one posting
	// into two in dedupe.
	test.Eq(t, "https://www.google.com/about/careers/applications/jobs/results/94266795891794630", posting.URL)

	test.Eq(t, jobpostings.PostingSource{Platform: DirectPlatform, Key: "google"}, posting.Source)

	// The earliest of the three unlabelled timestamps, the latest as the update.
	test.Eq(t, time.Unix(1785148642, 0).UTC(), posting.PostedAt)
	test.Eq(t, time.Unix(1785148644, 0).UTC(), posting.UpdatedAt)
}

// TestGoogleReadsTheTemplatedPayLine covers the pay format the shared prose
// parser cannot read.
//
// Google templates "US: $86000 - $118000 (USD) + 15% bonus target" into the
// description. Its only cue word is the "pay" in "Individual pay is determined
// by", which is not one of ParseCompensationFromText's cues, so every one of
// these — 185 of 295 sampled postings — parses to nothing without this.
func TestGoogleReadsTheTemplatedPayLine(t *testing.T) {
	t.Parallel()

	stub := &googleStub{maxRequests: 5, pages: map[int]string{
		1: googleFixturePage(1, "2", googleRecordJSON(
			"1", "Data Center Operations Technician",
			"https://www.google.com/about/careers/applications/signin?jobId=abc",
			"Google", googleUSLocation,
			`Individual pay is determined by factors including job-related skills.  <br><br>US: $86000 - $118000 (USD) + 15% bonus target + equity + benefits<br><br>`,
			1785148642,
		)),
	}}

	postings, errs := googleDrain(t, stub)

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	must.NotNil(t, postings[0].Compensation)

	pay := postings[0].Compensation

	test.Eq(t, float64(86000), pay.Min)
	test.Eq(t, float64(118000), pay.Max)
	test.Eq(t, "USD", pay.Currency)
	test.Eq(t, jobpostings.PeriodYear, pay.Period)

	// Google's own rendering is kept because it carries what the numbers cannot.
	test.StrContains(t, pay.Summary, "15% bonus target")

	// It came out of the description, and must not be presented as though the
	// board published it in a dedicated field.
	test.Eq(t, jobpostings.ProvenanceDescription, pay.Provenance)
}

// TestGoogleDeclinesAnUnresolvableTwoCountryRange is the conservative half of
// the pay reading.
//
// A posting open in two countries publishes one range per country, in no fixed
// order — the sampled examples led with Canada as often as with the US. When
// the posting is open in both, nothing in the payload says which range a reader
// should be shown, so neither is published. Picking the first would report a
// Canadian salary for a US role on whichever postings happen to list Canada
// first.
func TestGoogleDeclinesAnUnresolvableTwoCountryRange(t *testing.T) {
	t.Parallel()

	const twoRanges = `Individual pay is determined by factors including job-related skills.  <br><br>` +
		`US: $147000 - $211000 (USD) + 15% bonus target + equity + benefits<br><br>` +
		`Canada: $150000 - $154000 (CAD) + 15% bonus target + equity + benefits<br><br>`

	t.Run("open in both countries", func(t *testing.T) {
		t.Parallel()

		stub := &googleStub{maxRequests: 5, pages: map[int]string{
			1: googleFixturePage(1, "2", googleRecordJSON(
				"1", "AI Software Developer, Android XR",
				"https://www.google.com/about/careers/applications/signin?jobId=abc",
				"Google", googleBothLocations, twoRanges, 1785148642,
			)),
		}}

		postings, errs := googleDrain(t, stub)

		must.SliceEmpty(t, errs)
		must.Len(t, 1, postings)
		test.Nil(t, postings[0].Compensation)
	})

	// One country, two ranges: the location does single one out, so it is read.
	t.Run("resolvable against the posting's country", func(t *testing.T) {
		t.Parallel()

		stub := &googleStub{maxRequests: 5, pages: map[int]string{
			1: googleFixturePage(1, "2", googleRecordJSON(
				"1", "AI Software Developer, Android XR",
				"https://www.google.com/about/careers/applications/signin?jobId=abc",
				"Google", googleCALocation, twoRanges, 1785148642,
			)),
		}}

		postings, errs := googleDrain(t, stub)

		must.SliceEmpty(t, errs)
		must.Len(t, 1, postings)
		must.NotNil(t, postings[0].Compensation)

		test.Eq(t, "CAD", postings[0].Compensation.Currency)
		test.Eq(t, float64(150000), postings[0].Compensation.Min)
	})
}

// TestGoogleFallsBackToProseForTheOlderPayWording covers the other live format.
func TestGoogleFallsBackToProseForTheOlderPayWording(t *testing.T) {
	t.Parallel()

	stub := &googleStub{maxRequests: 5, pages: map[int]string{
		1: googleFixturePage(1, "2", googleRecordJSON(
			"1", "Director, Something",
			"https://www.google.com/about/careers/applications/signin?jobId=abc",
			"Google", googleUSLocation,
			`<p>The US base salary range for this full-time position is $275,850 - $326,000 + 25% bonus target + equity + benefits.</p>`,
			1785148642,
		)),
	}}

	postings, errs := googleDrain(t, stub)

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	must.NotNil(t, postings[0].Compensation)

	test.Eq(t, float64(275850), postings[0].Compensation.Min)
	test.Eq(t, float64(326000), postings[0].Compensation.Max)
}

// TestGoogleSkipsSignpostsWithoutEndingTheBoard is a regression test for a
// silent truncation this adapter actually shipped in development.
//
// Three records on the Google board are signposts rather than openings ("Open
// Engineering Career Opportunities, CapitalG Portfolio Companies") and publish a
// null apply URL. The layout guard originally treated a null apply URL as proof
// the record layout had changed, so the first one aborted the whole company:
// 1,059 of 3,252 postings, deterministically, with the run reporting success.
//
// Both halves matter. The signpost must not be published — nobody can apply to
// it — and it must not stop the 2,190 real postings that come after it.
func TestGoogleSkipsSignpostsWithoutEndingTheBoard(t *testing.T) {
	t.Parallel()

	apply := "https://www.google.com/about/careers/applications/signin?jobId=abc"

	stub := &googleStub{maxRequests: 6, pages: map[int]string{
		1: googleFixturePage(3, "2",
			googleRecordJSON("1", "Software Engineer", apply, "Google", googleUSLocation, "<p>Work.</p>", 1785148642),
			googleRecordJSON("2", "Open Engineering Career Opportunities, CapitalG Portfolio Companies", "", "Google", googleUSLocation, "<p>Signpost.</p>", 1785148642),
			googleRecordJSON("3", "Site Reliability Engineer", apply, "Google", googleUSLocation, "<p>Work.</p>", 1785148642),
		),
	}}

	postings, errs := googleDrain(t, stub)

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	test.Eq(t, "Software Engineer", postings[0].Title)
	test.Eq(t, "Site Reliability Engineer", postings[1].Title)
}

// TestGoogleRefusesAReshapedRecord is the other side of the same trade-off.
//
// The payload is a positional array with no field names, so a field inserted
// upstream shifts every index after it and this adapter would publish one
// posting's title against another's id with nothing failing. The layout is
// therefore recognised by anchors that every one of the 3,252 live records
// satisfies, and a record that fails them is an error rather than a posting.
func TestGoogleRefusesAReshapedRecord(t *testing.T) {
	t.Parallel()

	// The live record with one field inserted at the front, which is exactly
	// what a new field upstream would look like.
	shifted := `["INSERTED","94266795891794630","Staff Software Engineer",` +
		`"https://www.google.com/about/careers/applications/signin?jobId=abc",` +
		`[null,"<ul></ul>"],[null,"<ul></ul>"],` +
		`"projects/gweb-careers-proto/tenants/t/companies/c",null,"Google","en-US",` +
		googleUSLocation + `,[null,"<p>Work.</p>"],[2],[1785148642,0],[1785148642,0],[1785148642,0]]`

	stub := &googleStub{maxRequests: 5, pages: map[int]string{
		1: googleFixturePage(1, "2", shifted),
	}}

	postings, errs := googleDrain(t, stub)

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "record layout has changed")
}

// TestGooglePaginatesToTheEndOfTheBoard checks that a short page is not what
// ends pagination.
//
// The live board's last page is short (12 of 20) and is genuinely the end, which
// makes "stop on a short page" look correct and cost nothing — until a board
// serves a short page in the middle, at which point the company is truncated and
// the crawl reports success. That is the shape that capped every Workday tenant
// at 80 postings. Only a page with no records at all ends this loop.
func TestGooglePaginatesToTheEndOfTheBoard(t *testing.T) {
	t.Parallel()

	apply := "https://www.google.com/about/careers/applications/signin?jobId=abc"

	record := func(id string) string {
		return googleRecordJSON(id, "Engineer "+id, apply, "Google", googleUSLocation, "<p>Work.</p>", 1785148642)
	}

	full := make([]string, 0, 20)
	for i := range 20 {
		full = append(full, record(fmt.Sprint("1", i)))
	}

	stub := &googleStub{maxRequests: 8, pages: map[int]string{
		1: googleFixturePage(43, "2", full...),
		// Short, and NOT the end of the board.
		2: googleFixturePage(43, "2", record("200"), record("201"), record("202")),
		3: googleFixturePage(43, "2", record("300"), record("301")),
	}}

	postings, errs := googleDrain(t, stub)

	must.SliceEmpty(t, errs)
	test.Len(t, 25, postings, test.Sprint("a short page in the middle of a board must not end pagination"))
}

// TestGoogleStopsWhenTheBoardRepeatsItself bounds a board that ignores "page".
func TestGoogleStopsWhenTheBoardRepeatsItself(t *testing.T) {
	t.Parallel()

	apply := "https://www.google.com/about/careers/applications/signin?jobId=abc"

	full := make([]string, 0, 20)
	for i := range 20 {
		full = append(full, googleRecordJSON(fmt.Sprint(i), "Engineer", apply, "Google", googleUSLocation, "<p>Work.</p>", 1785148642))
	}

	// Every page is the same page, and the board claims far more postings than
	// it will ever serve, so the reported total can never end the loop. Only
	// noticing that a page introduced nothing new does.
	page := googleFixturePage(3000, "2", full...)

	pages := map[int]string{}
	for i := 1; i <= 40; i++ {
		pages[i] = page
	}

	stub := &googleStub{maxRequests: 40, pages: pages}

	postings, errs := googleDrain(t, stub)

	must.SliceEmpty(t, errs)
	test.Len(t, 20, postings)
	test.Less(t, 4, stub.requests, test.Sprint("a board that repeats one page should be recognised almost immediately"))
}

// TestGoogleBoardFiltersAreDisplayNames guards a silent-zero failure.
//
// The careers search matches ?company= against the company's display name, case
// sensitively: "Google" returns 3,252 postings and "google" returns zero — with
// HTTP 200 and a well-formed, empty payload either way. Lower-casing these to
// match the registry keys beside them is an inviting tidy-up that would disable
// every Alphabet board while every test that does not touch the network kept
// passing.
func TestGoogleBoardFiltersAreDisplayNames(t *testing.T) {
	t.Parallel()

	for _, board := range googleBoards {
		if board.filter == strings.ToLower(board.filter) {
			t.Errorf("board %q has filter %q, which is all lower case; the careers search matches the display name case sensitively and would return nothing",
				board.company, board.filter)
		}

		if !strings.EqualFold(board.company, board.filter) {
			t.Errorf("board %q has filter %q, which is not the same name in a different case", board.company, board.filter)
		}
	}
}
