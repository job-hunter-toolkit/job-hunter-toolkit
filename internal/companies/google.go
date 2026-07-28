package companies

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	jobpostings "github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// The Alphabet boards, keyed the way the registry keys every other source:
// lower case, because the company list is sorted case-insensitively and
// `--company deepmind` is compared against these strings verbatim.
const (
	googleCompany   = "google"
	deepmindCompany = "deepmind"
	youtubeCompany  = "youtube"
)

// googleSearchURL is the careers search this adapter reads.
//
// The endpoint in the original issue, careers.google.com/api/jobs/jobs-v1/search,
// still 301s — to www.google.com/about/careers/applications/api/jobs/jobs-v1/search,
// which 404s. The redirect is a blanket path rewrite of the retired host, not a
// surviving API, so following it produces an adapter that fails on every run.
const googleSearchURL = "https://www.google.com/about/careers/applications/jobs/results/"

// googleJobURL is the public posting page, which takes the job id alone: the
// human-facing links carry a title slug after it ("...4382-software-engineer"),
// and the route ignores everything past the id (verified live: the bare id
// returns the same 200 page as the slugged form).
//
// The id-only form is deliberate for dedupe. [jobpostings.DedupeKey] keys on
// URL, and a slug derived from a title would change the moment Google reworded
// the title, silently splitting one posting into two across a day boundary.
const googleJobURL = googleSearchURL + "%s"

// googleBoards are the Alphabet companies this adapter crawls, and the exact
// string each one is filtered by.
//
// The filter is the company's *display name*, and it is case-sensitive:
// `?company=Google` returns 3,252 postings and `?company=google` returns zero,
// with HTTP 200 and a well-formed empty payload either way. A slug-cased filter
// is therefore the precise failure docs/adding-a-source.md warns about — a
// source that looks healthy and returns nothing — so these are written the way
// the site writes them and are checked by TestGoogleBoardFiltersAreDisplayNames.
//
// Four further companies share this board and are deliberately absent. GFiber,
// Verily Life Sciences, Waymo and Wing each publish exactly one posting, titled
// "Open Career Opportunities, <Company>", which is a signpost to that
// subsidiary's own careers site rather than a job anyone can apply for. Adding
// them would spend four sources and four requests per crawl to produce four
// rows that are not openings.
var googleBoards = []struct {
	// company is the registry identifier; filter is what the site matches on.
	company, filter string
}{
	{googleCompany, "Google"},
	{deepmindCompany, "DeepMind"},
	{youtubeCompany, "YouTube"},
}

// googlePageSize is how many postings one search page carries.
//
// Fixed by the server at 20 and not negotiable: page_size, pageSize, num,
// limit and results_per_page were each tried against a live board and all five
// were ignored, with the payload continuing to report 20. It is declared here
// because it bounds the page count, not because it can be changed.
const googlePageSize = 20

// googleMaxPages bounds how many pages one board may be asked for.
//
// The largest board is Google itself at ~3,250 postings, or ~163 pages, so this
// allows an order of magnitude of growth. It is a backstop for a board that
// keeps serving full pages forever, not a coverage limit: pagination normally
// ends on the reported total, and a board that hit this cap would be a bug
// worth seeing rather than a crawl worth truncating, which is why reaching it
// yields an error instead of stopping quietly.
const googleMaxPages = 1000

// googleDataKeyMarker and googleDataMarker locate the search payload inside the
// results page.
//
// The page is server-rendered and its search results are embedded in a
// `AF_initDataCallback({key: 'ds:1', hash: '2', data:[...]});` call so the list
// has content before the page's JavaScript runs. The hash value changes between
// deploys, so the key is matched and the payload is then found by its own label
// rather than by a fixed offset.
const (
	googleDataKeyMarker = `key: 'ds:1'`
	googleDataMarker    = `data:`
)

// Positions of the fields this adapter reads inside a posting record.
//
// The record is a positional JSON array with no names in it, which is the one
// genuinely dangerous property of this payload: if Google inserts a field, every
// index after it shifts and this adapter would publish one posting's title
// against another's id without anything failing. [googleRecordIsWellFormed] is
// the guard against exactly that, and it is why these are read through it rather
// than directly.
const (
	googleFieldID          = 0
	googleFieldTitle       = 1
	googleFieldApplyURL    = 2
	googleFieldCompanyPath = 5
	googleFieldCompany     = 7
	googleFieldLocations   = 9
	googleFieldDescription = 10

	// The three timestamps. The payload does not say which is which, so they
	// are not read individually; see [googleTimestamps].
	googleFieldTimeFirst = 12
	googleFieldTimeLast  = 14
)

// googleCompanyPathPrefix is the resource name every posting carries, and the
// anchor [googleRecordIsWellFormed] recognises the record's layout by.
//
// It is a Google Cloud Talent Solution resource path,
// "projects/gweb-careers-proto/tenants/<uuid>/companies/<uuid>", which is
// distinctive enough that finding it at its expected index is strong evidence
// the other indices still mean what they meant.
const googleCompanyPathPrefix = "projects/gweb-careers-proto/"

// googleSearchPage is one decoded page of results.
type googleSearchPage struct {
	// jobs is nil rather than empty on a page past the end: the outer array's
	// first element is literally null there, which is what ends pagination.
	jobs []googleRecord

	// total is the board's own count of matching postings, reported on every
	// page including the ones past the end.
	total int
}

// googleRecord is one posting, still as the positional array the payload uses.
//
// Kept as raw messages rather than decoded into a struct because the array is
// heterogeneous — strings, nested arrays, [seconds, nanos] pairs and nulls in
// one list — and because a single field arriving in an unexpected shape must
// cost that field rather than failing the decode of the whole page, which is
// the whole board.
type googleRecord []json.RawMessage

// googleOuter is the shape wrapping a page's records: [jobs, null, total, size].
const (
	googleOuterJobs  = 0
	googleOuterTotal = 2
)

// Sources for the Alphabet boards, added to the direct-employer registry.
func googleSources() []Source {
	sources := make([]Source, 0, len(googleBoards))

	for _, board := range googleBoards {
		sources = append(sources, Source{
			Key:     board.company,
			Company: board.company,
			Jobs:    GoogleBoard(board.company, board.filter),
		})
	}

	return sources
}

// GoogleBoard returns a fetch function for one Alphabet company's postings.
//
// company is the registry identifier the postings are labelled with, and filter
// is the display name the careers search matches on; see [googleBoards] for why
// those are two different strings.
func GoogleBoard(company, filter string) jobpostings.JobsFunc {
	return func(ctx context.Context, httpClient *http.Client) jobpostings.Jobs {
		return func(yield func(*jobpostings.JobPosting, error) bool) {
			seen := make(map[string]bool)

			for page := 1; page <= googleMaxPages; page++ {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				result, err := googlePage(ctx, httpClient, filter, page)
				if err != nil {
					yield(nil, err)
					return
				}

				// A page past the end carries no records and still reports the
				// total, so an empty list is the end of the board rather than a
				// failure.
				if len(result.jobs) == 0 {
					return
				}

				// Counted per page so that a board serving one page forever is
				// recognised from the page itself, rather than only when the
				// reported total happens to be reachable.
				var fresh int

				for _, record := range result.jobs {
					if ctx.Err() != nil {
						yield(nil, ctx.Err())
						return
					}

					if !googleRecordIsWellFormed(record) {
						yield(nil, fmt.Errorf("refusing to read a Google posting for %q: the search payload's record layout has changed", company))
						return
					}

					// Deduplicated on the record id rather than on whether a
					// posting was published, so that a page made entirely of
					// signposts still counts as new work and does not read as a
					// repeat of the previous page.
					//
					// The board pages cleanly — 3,252 records over 163 pages
					// yielded 3,249 distinct ids — but the search is re-run per
					// request, so a posting added while paginating shifts one
					// across a page boundary and it is served twice. Skipping
					// the repeat costs nothing; publishing it would put a
					// duplicate in front of anyone using --no-dedupe.
					id := googleText(record, googleFieldID)
					if seen[id] {
						continue
					}

					seen[id] = true
					fresh++

					posting := googlePosting(record, company)
					if posting == nil {
						continue
					}

					if !yield(posting, nil) {
						return
					}
				}

				// A page that introduces no record this crawl has not already
				// seen is a board serving the same page whatever "page" says.
				// The reported total alone does not catch that: a board that
				// repeats one page of 20 while claiming 3,000 postings would
				// otherwise be requested googleMaxPages times.
				if fresh == 0 {
					return
				}

				// The board's own total is the other bound.
				//
				// Pagination deliberately does NOT stop on a short page, which
				// is the usual shape of this loop and is wrong here. The last
				// page of the Google board is short (12 of 20) and so is the end
				// of the board, but a short page anywhere else would end the
				// crawl of a company at that point and report success — the
				// silent truncation that capped every Workday tenant at 80
				// postings. One extra request per board buys a terminator that
				// cannot be faked: a page past the end carries no records at all.
				if result.total > 0 && len(seen) >= result.total {
					return
				}
			}

			yield(nil, fmt.Errorf("refusing to keep paginating Google for %q: still serving full pages after %d pages of %d", company, googleMaxPages, googlePageSize))
		}
	}
}

// googlePage fetches and decodes one page of a board's search results.
func googlePage(ctx context.Context, httpClient *http.Client, filter string, page int) (*googleSearchPage, error) {
	query := url.Values{
		"company": []string{filter},
		"page":    []string{strconv.Itoa(page)},
	}

	requestURL := googleSearchURL + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Google board %q: %w", filter, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Google for board %q: %w", filter, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from Google for board %q: %s", filter, resp.Status)
	}

	body := &strings.Builder{}
	if _, err := io.Copy(body, resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response from Google for board %q: %w", filter, err)
	}

	return googleDecodePage(body.String(), filter)
}

// googleDecodePage pulls the search payload out of a results page's HTML.
func googleDecodePage(body, filter string) (*googleSearchPage, error) {
	keyAt := strings.Index(body, googleDataKeyMarker)
	if keyAt == -1 {
		return nil, fmt.Errorf("failed to find search results in Google page for board %q: page layout may have changed", filter)
	}

	dataAt := strings.Index(body[keyAt:], googleDataMarker)
	if dataAt == -1 {
		return nil, fmt.Errorf("failed to find the search payload in Google page for board %q: page layout may have changed", filter)
	}

	// The payload is a JavaScript argument inside an HTML document, not a JSON
	// document of its own. json.Decoder stops at the end of the first complete
	// value, so pointing it at the array's opening bracket is enough and the
	// script and markup that follow never need parsing.
	var outer []json.RawMessage

	rest := body[keyAt+dataAt+len(googleDataMarker):]
	if err := json.NewDecoder(strings.NewReader(rest)).Decode(&outer); err != nil {
		return nil, fmt.Errorf("failed to decode search results from Google for board %q: %w", filter, err)
	}

	page := &googleSearchPage{}

	// Both elements are read defensively rather than by asserting a length: a
	// page past the end of a board is [null, null, total, size], and the point
	// of reading the total there is to notice the board is finished.
	if len(outer) > googleOuterJobs {
		// A null jobs array is the documented end of pagination, so a decode
		// failure here is not treated as one: it leaves jobs nil, which the
		// caller reads as "no more pages".
		_ = json.Unmarshal(outer[googleOuterJobs], &page.jobs)
	}

	if len(outer) > googleOuterTotal {
		_ = json.Unmarshal(outer[googleOuterTotal], &page.total)
	}

	return page, nil
}

// googleRecordIsWellFormed reports whether a record's positional fields still
// hold what this adapter's index constants say they hold.
//
// This is the guard on the payload's one real hazard. Every other source here
// reads named JSON fields, so a renamed field costs an empty column and a
// re-shaped one costs a decode; a positional array has neither protection, and
// a single field inserted upstream would shift every index after it and publish
// each posting's data under the wrong heading, silently and at full volume.
//
// The check is deliberately about *shape*, not content: an id that is all
// digits, a Cloud Talent Solution resource path where one is expected, and a
// company name. All 3,252 records on the largest board satisfy all three, so a
// failure means this adapter no longer knows what it is reading, and the caller
// turns that into a visible error rather than a plausible-looking posting.
//
// The apply URL is deliberately *not* one of the anchors, though it looks like
// an obvious fourth. Three records on the Google board are signposts rather
// than openings ("Open Engineering Career Opportunities, CapitalG Portfolio
// Companies") and publish a null apply URL. Anchoring on it aborted the whole
// board at the first one: 1,059 of 3,252 postings, deterministically, reported
// as a clean run. An anchor has to be something every record has, or it detects
// unusual postings rather than a changed layout.
func googleRecordIsWellFormed(record googleRecord) bool {
	if len(record) <= googleFieldDescription {
		return false
	}

	id := googleText(record, googleFieldID)
	if id == "" || strings.ContainsFunc(id, func(r rune) bool { return r < '0' || r > '9' }) {
		return false
	}

	if googleText(record, googleFieldCompany) == "" {
		return false
	}

	return strings.HasPrefix(googleText(record, googleFieldCompanyPath), googleCompanyPathPrefix)
}

// googlePosting builds a posting from a record whose layout has already been
// checked, returning nil when the record carries nothing usable.
func googlePosting(record googleRecord, company string) *jobpostings.JobPosting {
	var (
		id    = googleText(record, googleFieldID)
		title = strings.TrimSpace(googleText(record, googleFieldTitle))
	)

	if id == "" || title == "" {
		return nil
	}

	// A record with no apply URL is a signpost, not an opening: the three on the
	// Google board point at CapitalG's portfolio companies, the way the four
	// Alphabet companies left out of [googleBoards] point at their own careers
	// sites. They would otherwise be published as Google jobs that cannot be
	// applied for.
	if googleText(record, googleFieldApplyURL) == "" {
		return nil
	}

	posting := &jobpostings.JobPosting{
		Company:    company,
		URL:        fmt.Sprintf(googleJobURL, id),
		Title:      title,
		Location:   googleLocation(record),
		ExternalID: id,
		Source:     jobpostings.PostingSource{Platform: DirectPlatform, Key: company},
	}

	if posted, updated, ok := googleTimestamps(record); ok {
		posting.PostedAt = posted
		posting.UpdatedAt = updated
	}

	if pay := googleCompensation(record); pay != nil {
		posting.Compensation = pay
	}

	return posting
}

// googleLocation renders a posting's place of work.
//
// A posting can carry many locations — one sampled posting listed 25 — and only
// the first is reported. That is a deliberate choice against the alternative
// Workday takes, which renders the same situation as the string "4 Locations":
// a real city is useful to someone filtering by location and "25 Locations" is
// useful to nobody.
func googleLocation(record googleRecord) string {
	var locations [][]json.RawMessage

	if !googleUnmarshal(record, googleFieldLocations, &locations) {
		return "unknown/remote"
	}

	for _, location := range locations {
		if len(location) == 0 {
			continue
		}

		var display string
		if err := json.Unmarshal(location[0], &display); err != nil {
			continue
		}

		if display = strings.TrimSpace(display); display != "" {
			return display
		}
	}

	return "unknown/remote"
}

// googleTimestamps reads a posting's timestamps, reporting false when none of
// them is readable.
//
// The payload carries three, as [seconds, nanos] pairs, and labels none of
// them. In every posting sampled they fell within the same second of each other,
// so rather than guess which index is the publish time and which the last
// update, the earliest is reported as PostedAt and the latest as UpdatedAt.
// Guessing wrong would put a publish time in an update field where nothing
// downstream could notice; this ordering is true whichever way round they are.
func googleTimestamps(record googleRecord) (posted, updated time.Time, ok bool) {
	for field := googleFieldTimeFirst; field <= googleFieldTimeLast; field++ {
		// A [seconds, nanos] pair. Nanos are discarded: no consumer here is
		// finer-grained than a day, and reading only the seconds cannot turn a
		// malformed second element into a wrong instant.
		var pair []int64

		if !googleUnmarshal(record, field, &pair) || len(pair) == 0 || pair[0] <= 0 {
			continue
		}

		at := time.Unix(pair[0], 0).UTC()

		if !ok || at.Before(posted) {
			posted = at
		}

		if !ok || at.After(updated) {
			updated = at
		}

		ok = true
	}

	return posted, updated, ok
}

// googlePayPattern matches the pay line Google templates into a description:
// "US: $86000 - $118000 (USD) + 15% bonus target + equity + benefits".
//
// The region label is required rather than optional. It is present on every one
// of the 269 such lines sampled across three boards, and it is what makes a
// posting that publishes two of them resolvable; see [googleCompensation].
//
// The trailing "+ ..." clauses are captured so they can be kept in
// [jobpostings.Compensation.Summary]. They are the part of the offer the numeric
// range cannot hold — the bonus target and whether equity is included — and they
// run to the line's closing tag, which is why the clause body stops at "<".
var googlePayPattern = regexp.MustCompile(`([A-Za-z][A-Za-z .]{0,23}):\s*\$([\d,]+)\s*(?:-|–|—)\s*\$([\d,]+)\s*\(([A-Z]{3})\)((?:\s*\+[^<+]*)*)`)

// googlePayFloor is the smallest figure this will read as an annual salary.
//
// Every one of the 269 sampled ranges fell between 73,500 and 328,000, so a
// year period is a reading of the data rather than a guess. The floor exists so
// it stays one: if Google ever templates an hourly rate into the same line, the
// range is still published but without a period, rather than being multiplied
// into a six-figure salary the employer never offered.
const googlePayFloor = 1000

// googleRegionCountries maps a pay line's region label to the country codes a
// posting's locations use, for the postings that publish more than one range.
var googleRegionCountries = map[string]string{
	"US":     "US",
	"Canada": "CA",
}

// googleCompensation reads a posting's pay range out of its description.
//
// Google publishes pay two ways, and both are handled here because both are
// live. The templated line above is the common one — 185 of 295 sampled
// postings — and the generic prose parser cannot read it: the line's only cue
// word is "pay" in "Individual pay is determined by...", which is not one of
// [jobpostings.ParseCompensationFromText]'s cues, so every one of those ranges
// parses to nothing. The older prose form ("The US base salary range for this
// full-time position is $275,850 - $326,000") does carry a cue, and is left to
// that parser rather than duplicated here.
func googleCompensation(record googleRecord) *jobpostings.Compensation {
	description := googleHTML(record, googleFieldDescription)
	if description == "" {
		return nil
	}

	matches := googlePayPattern.FindAllStringSubmatch(description, -1)

	switch len(matches) {
	case 0:
		// No templated line: fall back to the prose form, which the shared
		// parser already reads correctly.
		return jobpostings.ParseCompensationFromDescription(description)
	case 1:
	default:
		// A posting open in more than one country publishes one range per
		// country ("US: ... (USD)" and "Canada: ... (CAD)"), in no fixed order.
		// Taking the first would report a Canadian salary for a US role on
		// whichever postings happen to list Canada first, so the range is
		// resolved against where the job actually is, and left empty when that
		// does not single one out.
		matched := googleMatchRegionToLocation(matches, record)
		if matched == nil {
			return nil
		}

		matches = [][]string{matched}
	}

	return googlePayFromMatch(matches[0])
}

// googlePayFromMatch builds a compensation from one pay-line match.
func googlePayFromMatch(match []string) *jobpostings.Compensation {
	low, lowOK := googleAmount(match[2])
	high, highOK := googleAmount(match[3])

	if !lowOK || !highOK || high < low {
		return nil
	}

	pay := &jobpostings.Compensation{
		Min:      low,
		Max:      high,
		Currency: match[4],

		// Google's own rendering, kept because it carries what the numbers
		// cannot: the bonus target and whether equity is offered.
		Summary:    strings.TrimSpace(match[0]),
		Provenance: jobpostings.ProvenanceDescription,
	}

	if low >= googlePayFloor {
		pay.Period = jobpostings.PeriodYear
	}

	return pay
}

// googleMatchRegionToLocation picks the pay line whose region is where the
// posting is, returning nil unless exactly one qualifies.
func googleMatchRegionToLocation(matches [][]string, record googleRecord) []string {
	countries := googleCountries(record)

	var matched []string

	for _, match := range matches {
		country, known := googleRegionCountries[strings.TrimSpace(match[1])]
		if !known || !countries[country] {
			continue
		}

		if matched != nil {
			// Two ranges both apply. Nothing in the payload says which one a
			// reader should take, so neither is published.
			return nil
		}

		matched = match
	}

	return matched
}

// googleCountries collects the country codes a posting's locations name.
func googleCountries(record googleRecord) map[string]bool {
	countries := map[string]bool{}

	var locations [][]json.RawMessage

	if !googleUnmarshal(record, googleFieldLocations, &locations) {
		return countries
	}

	// The country code is the sixth element of a location entry, after the
	// display name, the address lines, the city, the postal code and the region.
	const googleLocationCountry = 5

	for _, location := range locations {
		if len(location) <= googleLocationCountry {
			continue
		}

		var country string
		if err := json.Unmarshal(location[googleLocationCountry], &country); err == nil && country != "" {
			countries[country] = true
		}
	}

	return countries
}

// googleAmount parses one figure from a pay line.
func googleAmount(raw string) (float64, bool) {
	amount, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
	if err != nil || amount <= 0 {
		return 0, false
	}

	return amount, true
}

// googleText reads a string field, returning "" for anything that is not one.
func googleText(record googleRecord, field int) string {
	var text string

	if !googleUnmarshal(record, field, &text) {
		return ""
	}

	return text
}

// googleHTML reads one of the payload's [null, "<html>"] wrapped bodies.
//
// The description, responsibilities and qualifications all arrive in that
// two-element form rather than as bare strings.
func googleHTML(record googleRecord, field int) string {
	var wrapper []json.RawMessage

	if !googleUnmarshal(record, field, &wrapper) {
		return ""
	}

	for _, element := range wrapper {
		var text string
		if err := json.Unmarshal(element, &text); err == nil && text != "" {
			return text
		}
	}

	return ""
}

// googleUnmarshal decodes one positional field, reporting false when the field
// is absent, null, or not the shape asked for.
//
// Every read of this payload goes through here. A field that arrives in an
// unexpected shape then costs that one field rather than the page it is on,
// which for a board of 3,250 postings is the difference between an empty column
// and a company vanishing from the crawl.
func googleUnmarshal(record googleRecord, field int, into any) bool {
	if field < 0 || field >= len(record) {
		return false
	}

	return json.Unmarshal(record[field], into) == nil
}
