package services

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestBrassRing(t *testing.T) {
	testSingle(t, "homedepot,25526,5032", BrassRing)
}

func TestBrassRing_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	testMultipleParallel(t, slices.Values(BrassRingGateways), BrassRing)
}

// brassRingPageTransport serves one body per requested page number.
//
// The shared fixtureTransport matches on a URL substring, and every page of a
// BrassRing search is a POST to the same URL, so a paginated fixture built on it
// would serve whichever route the map happened to iterate to first. Keying on
// the request body's pageNumber is the only thing that tells these pages apart.
type brassRingPageTransport struct {
	pages    map[string]string
	requests []string
}

func (b *brassRingPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	var decoded struct {
		PageNumber string `json:"pageNumber"`
		PageSize   string `json:"pageSize"`
		PartnerID  string `json:"partnerId"`
		SiteID     string `json:"siteId"`
	}

	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}

	b.requests = append(b.requests, req.URL.Host+"|"+decoded.PartnerID+"|"+decoded.SiteID+"|"+decoded.PageNumber+"|"+decoded.PageSize)

	page, ok := b.pages[decoded.PageNumber]
	if !ok {
		page = `{"JobsCount":0,"Jobs":{"Job":[]}}`
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(page)),
		Request:    req,
	}, nil
}

// brassRingJobJSON builds one job object with the given name-keyed questions.
func brassRingJobJSON(link string, questions map[string]string) string {
	parts := make([]string, 0, len(questions))

	for _, name := range slices.Sorted(maps.Keys(questions)) {
		value, err := json.Marshal(questions[name])
		if err != nil {
			panic(err)
		}

		parts = append(parts, `{"QuestionName":`+strconv.Quote(name)+`,"Value":`+string(value)+`}`)
	}

	return `{"Link":` + strconv.Quote(link) + `,"Questions":[` + strings.Join(parts, ",") + `]}`
}

// TestBrassRingParsesPostings covers the name-keyed question list, the
// per-gateway location mapping and the two things this platform does not
// publish: a posted date, and any location at all on most gateways.
func TestBrassRingParsesPostings(t *testing.T) {
	t.Parallel()

	page := `{"JobsCount":2,"Jobs":{"Job":[` +
		brassRingJobJSON("https://sjobs.brassring.com/TGnewUI/Search/home/HomeWithPreLoad?partnerid=25632&siteid=5649&PageType=JobDetails&jobid=1", map[string]string{
			"reqid":       "3748750",
			"autoreq":     "1844774BR",
			"jobtitle":    "Retail Sales Associate &amp; Greeter ",
			"lastupdated": "28-Jul-2026",
			"formtext12":  "Everett",
			"formtext10":  "Massachusetts ",
			"department":  "Retail ",
		}) + `,` +
		brassRingJobJSON("", map[string]string{
			"reqid":    "3749412",
			"jobtitle": "No link",
		}) +
		`]}}`

	transport := &brassRingPageTransport{pages: map[string]string{"1": page}}
	client := &http.Client{Transport: transport}

	postings, errs := drain(BrassRing(t.Context(), client, "bestbuy,25632,5649"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	posting := postings[0]

	test.Eq(t, "bestbuy", posting.Company)
	test.Eq(t, "Retail Sales Associate & Greeter", posting.Title)
	test.Eq(t, "Everett, Massachusetts", posting.Location)
	test.Eq(t, "Retail", posting.Department)
	test.Eq(t, "3748750", posting.ExternalID)
	test.Eq(t, "1844774BR", posting.RequisitionID)
	test.Eq(t, internal.PostingSource{Platform: "brassring", Key: "bestbuy,25632,5649"}, posting.Source)
	test.Eq(t, time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC), posting.UpdatedAt)

	// BrassRing publishes no first-published date anywhere in this response, so
	// PostedAt stays zero rather than being filled from lastupdated.
	test.True(t, posting.PostedAt.IsZero())

	// One page for two postings, at the server's own cap.
	must.Len(t, 1, transport.requests)
	test.Eq(t, "sjobs.brassring.com|25632|5649|1|50", transport.requests[0])
}

// TestBrassRingUsesTheGatewaysOwnLocationQuestion covers the default path, and
// the deliberate empty for a gateway that publishes no location at all.
func TestBrassRingUsesTheGatewaysOwnLocationQuestion(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		key       string
		questions map[string]string
		want      string
	}{
		"location question": {
			key:       "homedepot,25526,5032",
			questions: map[string]string{"jobtitle": "Cashier", "location": "1605 - NEW CASTLE "},
			want:      "1605 - NEW CASTLE",
		},
		"mapped override wins over a location question": {
			key:       "performancefoodgroup,26350,6930",
			questions: map[string]string{"jobtitle": "Driver", "location": "Performance Caro (0635) ", "formtext5": "Houma, Louisiana (LA) "},
			want:      "Houma, Louisiana (LA)",
		},
		"unmapped gateway publishes nothing rather than a guess": {
			key:       "northropgrumman,16030,6090",
			questions: map[string]string{"jobtitle": "Data Visualization Analyst", "formtext9": "Baltimore"},
			want:      "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := `{"JobsCount":1,"Jobs":{"Job":[` +
				brassRingJobJSON("https://sjobs.brassring.com/TGnewUI/Search/home/HomeWithPreLoad?jobid=1", testCase.questions) +
				`]}}`

			client := &http.Client{Transport: &brassRingPageTransport{pages: map[string]string{"1": page}}}

			postings, errs := drain(BrassRing(t.Context(), client, testCase.key))

			must.SliceEmpty(t, errs)
			must.Len(t, 1, postings)
			test.Eq(t, testCase.want, postings[0].Location)
		})
	}
}

// TestBrassRingPaginates checks the two things that bound the loop: JobsCount
// from the first page, and a short page.
func TestBrassRingPaginates(t *testing.T) {
	t.Parallel()

	job := func(id string) string {
		return brassRingJobJSON("https://sjobs.brassring.com/TGnewUI/Search/home/HomeWithPreLoad?jobid="+id,
			map[string]string{"jobtitle": "Cashier " + id, "reqid": id, "location": "Somewhere"})
	}

	page := func(total int, ids ...string) string {
		jobs := make([]string, 0, len(ids))
		for _, id := range ids {
			jobs = append(jobs, job(id))
		}

		return `{"JobsCount":` + strconv.Itoa(total) + `,"Jobs":{"Job":[` + strings.Join(jobs, ",") + `]}}`
	}

	transport := &brassRingPageTransport{pages: map[string]string{
		"1": page(5, "1", "2"),
		"2": page(5, "3", "4"),
		"3": page(5, "5"),
		// Page 4 would be served the empty default, and asking for it at all
		// would mean JobsCount was not honoured.
	}}

	client := &http.Client{Transport: transport}

	postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

	must.SliceEmpty(t, errs)
	must.Len(t, 5, postings)

	// JobsCount 5 over pages of 2 is three pages, and exactly three were asked
	// for: no request is spent discovering the end.
	must.Len(t, 3, transport.requests)
	test.Eq(t, "sjobs.brassring.com|25526|5032|3|50", transport.requests[2])
}

// TestBrassRingStopsOnARepeatedPage is the regression this package has been
// burned by: an adapter that stops only on a short page loops forever against a
// gateway that ignores its page parameter.
//
// The fixture also lies about JobsCount, so the only thing that can end the loop
// is recognising the repeat.
func TestBrassRingStopsOnARepeatedPage(t *testing.T) {
	t.Parallel()

	same := `{"JobsCount":100000,"Jobs":{"Job":[` +
		brassRingJobJSON("https://sjobs.brassring.com/TGnewUI/Search/home/HomeWithPreLoad?jobid=1",
			map[string]string{"jobtitle": "Cashier", "reqid": "1", "location": "Somewhere"}) +
		`]}}`

	pages := make(map[string]string, brassRingMaxPages)
	for page := 1; page <= brassRingMaxPages+5; page++ {
		pages[strconv.Itoa(page)] = same
	}

	transport := &brassRingPageTransport{pages: pages}
	client := &http.Client{Transport: transport}

	postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)
	must.Len(t, 2, transport.requests)
}

// TestBrassRingRejectsAMalformedGateway checks that a mis-transcribed key is an
// error naming the key, rather than a request built from half a tuple.
func TestBrassRingRejectsAMalformedGateway(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"homedepot", "homedepot,25526", "homedepot,,5032", ",25526,5032", "a,b,c,d,e"} {
		client := &http.Client{Transport: &brassRingPageTransport{}}

		postings, errs := drain(BrassRing(t.Context(), client, key))

		test.SliceEmpty(t, postings)
		must.Len(t, 1, errs, must.Sprintf("key %q", key))
		test.StrContains(t, errs[0].Error(), key)
	}
}

// TestBrassRingReportsHTTPFailures covers the two failures a wrong gateway
// produces: the HTTP 500 an unknown partnerid answers with, and a body that is
// not JSON.
func TestBrassRingReportsHTTPFailures(t *testing.T) {
	t.Parallel()

	t.Run("non-200", func(t *testing.T) {
		t.Parallel()

		client, transport := fixtureClient(map[string]string{"ProcessSortAndShowMoreJobs": `<html>Error</html>`})
		transport.status = http.StatusInternalServerError

		postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

		test.SliceEmpty(t, postings)
		must.Len(t, 1, errs)
		test.StrContains(t, errs[0].Error(), "homedepot,25526,5032")
	})

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()

		client, _ := fixtureClient(map[string]string{"ProcessSortAndShowMoreJobs": `{"Jobs":`})

		postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

		test.SliceEmpty(t, postings)
		must.Len(t, 1, errs)
		test.StrContains(t, errs[0].Error(), "homedepot,25526,5032")
	})
}

// TestBrassRingReportsAShapeChange checks the guard against the silently-empty
// failure: a page of jobs that decodes but carries none of the questions this
// adapter needs must be an error, not zero postings.
func TestBrassRingReportsAShapeChange(t *testing.T) {
	t.Parallel()

	page := `{"JobsCount":1,"Jobs":{"Job":[` +
		brassRingJobJSON("https://sjobs.brassring.com/x", map[string]string{"title": "Cashier"}) +
		`]}}`

	client := &http.Client{Transport: &brassRingPageTransport{pages: map[string]string{"1": page}}}

	postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "jobtitle")
}

// TestBrassRingEmptyGatewayIsNotAnError is the case the survey gets wrong: a
// gateway that exists but has nothing published answers HTTP 200 with JobsCount
// 0, not HTTP 500, and that is a company not hiring rather than a bad key.
// Twelve of the 46 probed pairs answered exactly this way.
func TestBrassRingEmptyGatewayIsNotAnError(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: &brassRingPageTransport{}}

	postings, errs := drain(BrassRing(t.Context(), client, "kodak,515,251"))

	test.SliceEmpty(t, errs)
	test.SliceEmpty(t, postings)
}

// TestBrassRingReadsBothSlashDateOrders covers the ambiguity this platform
// creates for itself, and the refusal to guess when the page cannot settle it.
func TestBrassRingReadsBothSlashDateOrders(t *testing.T) {
	t.Parallel()

	page := func(dates ...string) string {
		jobs := make([]string, 0, len(dates))

		for index, date := range dates {
			id := strconv.Itoa(index)
			jobs = append(jobs, brassRingJobJSON("https://sjobs.brassring.com/x?jobid="+id, map[string]string{
				"jobtitle":    "Cashier " + id,
				"reqid":       id,
				"lastupdated": date,
			}))
		}

		return `{"JobsCount":` + strconv.Itoa(len(dates)) + `,"Jobs":{"Job":[` + strings.Join(jobs, ",") + `]}}`
	}

	for name, testCase := range map[string]struct {
		dates []string
		want  time.Time
	}{
		// 27 cannot be a month, so this whole page is month-first.
		"month first": {dates: []string{"07/09/2026", "07/27/2026"}, want: time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)},
		// 27 cannot be a month here either, so this page is day-first.
		"day first": {dates: []string{"09/07/2026", "27/07/2026"}, want: time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)},
		// Nothing on the page exceeds 12, so the order is unknowable and no date
		// is published for it.
		"ambiguous": {dates: []string{"07/09/2026", "08/10/2026"}, want: time.Time{}},
		// The unambiguous spelling never depends on the rest of the page.
		"named month": {dates: []string{"09-Jul-2026"}, want: time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := &http.Client{Transport: &brassRingPageTransport{pages: map[string]string{"1": page(testCase.dates...)}}}

			postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

			must.SliceEmpty(t, errs)
			must.SliceNotEmpty(t, postings)
			test.Eq(t, testCase.want, postings[0].UpdatedAt)
		})
	}
}

// TestBrassRingCarriesTheDateOrderAcrossPages is the case that made this
// gateway-wide rather than per-page.
//
// BrassRing sorts by date, so a page's fifty postings routinely share one value:
// Home Depot's page 100 is fifty copies of "11/07/2025" and its page 450 fifty
// copies of "11/09/2016", neither of which settles month-first from day-first on
// its own. Judged per page, only 53% of that gateway's 22,751 postings got a
// date. Page 1 settles it, and the rest inherit that.
func TestBrassRingCarriesTheDateOrderAcrossPages(t *testing.T) {
	t.Parallel()

	job := func(id, date string) string {
		return brassRingJobJSON("https://sjobs.brassring.com/x?jobid="+id, map[string]string{
			"jobtitle": "Cashier " + id, "reqid": id, "lastupdated": date,
		})
	}

	transport := &brassRingPageTransport{pages: map[string]string{
		// 27 cannot be a month, so this page is month-first.
		"1": `{"JobsCount":2,"Jobs":{"Job":[` + job("1", "07/27/2026") + `]}}`,
		// Nothing on this page settles anything by itself.
		"2": `{"JobsCount":2,"Jobs":{"Job":[` + job("2", "11/07/2025") + `]}}`,
	}}

	client := &http.Client{Transport: transport}

	postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	test.Eq(t, time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC), postings[0].UpdatedAt)
	test.Eq(t, time.Date(2025, time.November, 7, 0, 0, 0, 0, time.UTC), postings[1].UpdatedAt)
}

// brassRingFixture reads a page captured from a live BrassRing gateway.
//
// The capture under testdata is what the gateway answered with the first three
// of its fifty jobs kept and the one description-bearing question blanked —
// Home Depot's formtext5 and Best Buy's jobdescription, together about 90% of
// the bytes, neither of which this adapter decodes. Every other key, and every
// other value, is the gateway's own.
func brassRingFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// TestBrassRingParsesACapturedLiveGateway is the fixture that decides whether
// this adapter reads BrassRing, as opposed to reading the shape a document said
// BrassRing has. The bodies are page 1 of Home Depot's gateway (25526/5032) and
// Best Buy's (25632/5649) as captured on 2026-07-28.
//
// What the captures establish, and what the hand-written fixtures above cannot:
//
//   - the descriptions are not under "jobdescription" everywhere.
//     docs/research/ats-platform-survey.md names that question; Home Depot's
//     gateway does not send it at all and puts the body in formtext5. It does
//     not matter here because this project stores no descriptions, but it is the
//     same lesson as the location: question names are gateway configuration.
//   - "lastupdated" is "07/27/2026" on Home Depot, not the "17-Jun-2026" the
//     survey documents, and "28-Jul-2026" on Best Buy. Two formats on one
//     platform, in one capture.
//   - Best Buy really has no location question. The survey records this gateway
//     as one whose "location field is unmapped and parses empty"; page 1 shows
//     the city in formtext12 and the state in formtext10, which is what
//     [brassRingLocationQuestions] now carries.
//   - JobsCount is the whole gateway, not the page: 23,965 alongside three jobs.
func TestBrassRingParsesACapturedLiveGateway(t *testing.T) {
	t.Parallel()

	t.Run("homedepot", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: &brassRingPageTransport{pages: map[string]string{
			"1": brassRingFixture(t, "brassring_homedepot_search_page1.json"),
		}}}

		postings, errs := drain(BrassRing(t.Context(), client, "homedepot,25526,5032"))

		must.SliceEmpty(t, errs)
		must.Len(t, 3, postings)

		first := postings[0]

		test.Eq(t, "homedepot", first.Company)
		test.Eq(t, "Repair and Tool Technician", first.Title)
		test.Eq(t, "https://sjobs.brassring.com/TGnewUI/Search/home/HomeWithPreLoad?partnerid=25526&siteid=5032&PageType=JobDetails&jobid=158283", first.URL)
		test.Eq(t, "0179 - POOLER", first.Location)
		test.Eq(t, "158283", first.ExternalID)
		test.Eq(t, internal.PostingSource{Platform: "brassring", Key: "homedepot,25526,5032"}, first.Source)

		// "07/27/2026": the page settles month-first because 27 cannot be a month.
		test.Eq(t, time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC), first.UpdatedAt)

		// This gateway sends no autoreq at all, so the field stays absent rather
		// than being filled from the BrassRing-internal reqid.
		test.Eq(t, "", first.RequisitionID)
	})

	t.Run("bestbuy", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: &brassRingPageTransport{pages: map[string]string{
			"1": brassRingFixture(t, "brassring_bestbuy_search_page1.json"),
		}}}

		postings, errs := drain(BrassRing(t.Context(), client, "bestbuy,25632,5649"))

		must.SliceEmpty(t, errs)
		must.Len(t, 3, postings)

		first := postings[0]

		test.Eq(t, "Retail Sales Associate", first.Title)
		test.Eq(t, "Everett, Massachusetts", first.Location)
		test.Eq(t, "3748750", first.ExternalID)
		test.Eq(t, time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC), first.UpdatedAt)

		test.Eq(t, "Cerritos, California", postings[1].Location)
		test.Eq(t, "ONTARIO, California", postings[2].Location)
	})
}

// TestBrassRingRegisteredGatewaysComeFromTheCandidateFile keeps the registered
// list traceable, and additionally checks the two invariants a comma-keyed
// registry needs: every entry parses, and no (partnerid, siteid) pair is
// registered twice under two names.
func TestBrassRingRegisteredGatewaysComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "brassring_gateways.txt")

	must.Greater(t, 20, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	tenancies := make(map[string]string, len(BrassRingGateways))

	for _, key := range BrassRingGateways {
		gateway, err := parseBrassRingGateway(key)
		must.NoError(t, err, must.Sprintf("registered gateway %q does not parse", key))

		test.True(t, candidates[key], test.Sprintf("registered gateway %q is not in testdata/candidates/brassring_gateways.txt", key))

		previous, seen := tenancies[gateway.tenancy()]
		test.False(t, seen, test.Sprintf("gateway %q duplicates the tenancy already registered as %q", key, previous))

		tenancies[gateway.tenancy()] = key
	}
}

// TestBrassRingLocationMappingsBelongToRegisteredGateways stops the location
// table drifting into a list of tenancies nothing crawls: every mapping must
// belong to a gateway this binary actually fetches, so a mapping cannot outlive
// the gateway it was measured against.
func TestBrassRingLocationMappingsBelongToRegisteredGateways(t *testing.T) {
	t.Parallel()

	registered := make(map[string]bool, len(BrassRingGateways))

	for _, key := range BrassRingGateways {
		gateway, err := parseBrassRingGateway(key)
		must.NoError(t, err)

		registered[gateway.tenancy()] = true
	}

	for tenancy, questions := range brassRingLocationQuestions {
		test.True(t, registered[tenancy], test.Sprintf("location mapping for %q has no registered gateway", tenancy))
		test.SliceNotEmpty(t, questions, test.Sprintf("location mapping for %q is empty", tenancy))
	}
}
