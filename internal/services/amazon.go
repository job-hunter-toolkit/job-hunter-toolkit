package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
)

// amazonPlatform is the platform name this file registers under, shared with
// the [internal.PostingSource] every posting carries so the two cannot drift
// apart.
//
// Amazon runs its own careers site rather than a shared ATS — the largest
// employer this project had not covered, in docs/source-backlog.md since the
// beginning — so this platform has exactly one company. It lives here rather
// than in internal/companies because the board is an iCIMS-backed JSON search
// API (every posting carries an id_icims and the whole site is one stable
// endpoint), which is the durable API shape this package holds, not the
// redesign-fragile HTML scraping that package exists to quarantine.
const amazonPlatform = "amazon"

// amazonHost serves every search request and every canonical posting URL.
// Subsidiaries ride the same endpoint: AWS is the largest business_category on
// it (7,731 of 22,346 postings measured 2026-07-30) and Whole Foods Market's
// corporate roles answer a search for the brand (97 postings, all published by
// "Amazon.com Services LLC"). Whole Foods' in-store hourly hiring is a separate
// system elsewhere and is not covered here.
const amazonHost = "www.amazon.jobs"

func init() {
	registerBuiltin(amazonPlatform, multiJobsFunc(Amazon, AmazonCompanies))

	// One employer, one hostname — but a full crawl of it is ~300 sequential
	// requests (see the walk below), which is the www.google.com shape: a
	// single host that must not be hit unpaced just because it has no suffix
	// arm in httpx.servicePolicyFor. Registering it as a (single-host) shared
	// backend buys the same 4-concurrent / 25ms / 10s-cooldown policy the other
	// JSON backends get, from this file, without a second hand-maintained list
	// in httpx. An explicit host arm there would be the tidier home; reported
	// as a wanted httpx change rather than made here.
	httpx.RegisterSharedBackend(amazonPlatform, amazonHost)
}

// AmazonCompanies holds the single company this platform serves. It exists so
// the standard registration, health and filtering machinery treat Amazon like
// every other source rather than as a special case.
var AmazonCompanies = []string{"amazon"}

const (
	// amazonPageSize is the most postings one page may return, measured: a
	// request for more answers HTTP 200 with zero jobs and the server's own
	// words in its error field, "Result limit cannot be greater than 100".
	amazonPageSize = 100

	// amazonMaxWindow is the deepest row the search will serve, measured:
	// offset+result_limit past 10,000 answers 200 with zero jobs and
	// "Cannot return more than 10000 results at once", and the response's
	// "hits" field itself never reports more than 10,000 (an unfiltered query
	// answers exactly 10,000 hits while the facet counts on the same response
	// sum to 22,346). The same number, and the same Elasticsearch shape, as
	// [oracleCloudMaxWindow] and radancyMaxWindow.
	//
	// Unlike those two platforms the window does not bound coverage here,
	// because this search can be partitioned: see [Amazon] for how slices keep
	// every request inside it.
	amazonMaxWindow = 10000

	// amazonMaxPagesPerSlice bounds one slice's walk, derived from the window
	// so the two cannot drift. It is unconditional: a slice that ignores its
	// offset parameter is stopped sooner by [pageRepeatGuard], and one that
	// never runs out of distinct rows stops here, at the window edge.
	amazonMaxPagesPerSlice = amazonMaxWindow / amazonPageSize
)

// amazonSearchURL is one call to the search API.
//
// The two filter parameters mirror the facet names the same API reports, and
// both are exact: filtering on a facet value answers precisely that facet's
// count. category and country may each be empty, which omits the filter.
func amazonSearchURL(offset, limit int, category, country string, facets bool) string {
	var query strings.Builder

	query.WriteString("https://" + amazonHost + "/en/search.json?base_query=")
	query.WriteString("&offset=" + strconv.Itoa(offset))
	query.WriteString("&result_limit=" + strconv.Itoa(limit))

	if category != "" {
		query.WriteString("&business_category%5B%5D=" + url.QueryEscape(category))
	}

	if country != "" {
		query.WriteString("&normalized_country_code%5B%5D=" + url.QueryEscape(country))
	}

	if facets {
		query.WriteString("&facets%5B%5D=business_category&facets%5B%5D=normalized_country_code")
	}

	return query.String()
}

// amazonSearch is the subset of a search.json response this adapter uses.
//
// Error is the server's own failure channel: the API answers HTTP 200 with
// zero jobs and prose in this field both past the result window and past the
// page-size cap, so ignoring it would make those states indistinguishable from
// an exhausted slice.
type amazonSearch struct {
	Error string `json:"error"`
	Hits  int    `json:"hits"`

	Facets struct {
		BusinessCategory []map[string]int `json:"business_category_facet"`
		Country          []map[string]int `json:"normalized_country_code_facet"`
	} `json:"facets"`

	Jobs []struct {
		// IDIcims is the posting's identifier in the iCIMS system behind this
		// site, a string on the wire, and the {id} in the canonical
		// /en/jobs/{id}/{slug} path.
		IDIcims string `json:"id_icims"`

		Title string `json:"title"`

		// JobPath is the posting's canonical path on www.amazon.jobs. The
		// response also carries url_next_step, an apply redirect into
		// account.amazon.jobs, which is deliberately never used: an apply URL
		// in place of the posting's own page is the single mistake behind every
		// double count found in this repo.
		JobPath string `json:"job_path"`

		Location           string `json:"location"`
		NormalizedLocation string `json:"normalized_location"`

		// JobCategory is the readable org unit ("Software Development");
		// JobFamily the finer grouping inside it ("Fulfillment Center").
		JobCategory string `json:"job_category"`
		JobFamily   string `json:"job_family"`

		// JobScheduleType is "full-time" / "part-time", normalized through
		// [internal.NormalizeEmploymentType] like every other platform.
		JobScheduleType string `json:"job_schedule_type"`

		// PostedDate is US-English prose ("June 17, 2026", day space-padded).
		// The response's other timestamp, updated_time, is relative prose
		// ("5 minutes") and is not read.
		PostedDate string `json:"posted_date"`
	} `json:"jobs"`
}

// No compensation is published by this adapter, and that is measured rather
// than assumed: the search response has no structured pay field, and its
// description/description_short prose is a truncated summary that carried no
// dollar figure on any of 30 postings sampled across three business categories
// on 2026-07-30 (Amazon's US pay disclosures live only on the posting's detail
// page). Reading pay would cost one request per posting against a board of
// ~22,000, which is the trade this adapter's whole design refuses.

// amazonFacet is one facet bucket: a value and how many postings carry it.
type amazonFacet struct {
	Name  string
	Count int
}

// amazonFacets flattens the API's one-pair-per-object facet encoding
// ([{"aws": 7731}, {"finance": 769}, ...]) preserving the server's order, so a
// crawl visits slices in a stable sequence.
func amazonFacets(raw []map[string]int) []amazonFacet {
	facets := make([]amazonFacet, 0, len(raw))

	for _, entry := range raw {
		for name, count := range entry {
			facets = append(facets, amazonFacet{Name: name, Count: count})
		}
	}

	return facets
}

// amazonFacetTotal sums a facet's counts, which is the only place the board's
// real size is published: "hits" saturates at the result window.
func amazonFacetTotal(facets []amazonFacet) int {
	total := 0

	for _, facet := range facets {
		total += facet.Count
	}

	return total
}

// amazonPostedLayouts parse posted_date. "January _2, 2006" first because the
// API space-pads the day ("July  2, 2026"), which the unpadded layout rejects.
var amazonPostedLayouts = []string{"January _2, 2006", "January 2, 2006"}

// amazonTime parses a posted_date, returning the zero time for anything that
// is not one. UTC because the API publishes no zone, same as every adapter.
func amazonTime(value string) time.Time {
	value = strings.Join(strings.Fields(value), " ")

	for _, layout := range amazonPostedLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// Amazon returns all of the job postings on www.amazon.jobs, or an error if
// there was a problem making a request or parsing a response.
//
// # Why the walk is sliced
//
// The board is ~2.2x its own result window: facet counts summed to 22,346
// postings on 2026-07-30 while no query, however filtered, will serve a row
// past offset 10,000. Radancy and Oracle hit this same wall and live with
// truncation; here the search accepts exact filters over the same facets it
// reports, so the window is avoidable rather than a cap:
//
//  1. one request fetches the business_category and country facets;
//  2. each business_category is walked as its own slice — the largest (aws,
//     7,731) fits inside the window with headroom;
//  3. a category at or past the window (none today) is sub-sliced by country
//     before walking, the largest such intersection measured being USA×aws at
//     5,255.
//
// business_category is the dimension because it partitions: its counts sum to
// exactly the country facet's sum, missing values land in a real
// "no-business-category" bucket that the filter accepts like any other value,
// and the two sums are cross-checked on every crawl — a partition that stops
// covering the whole board is reported rather than silently under-crawled.
//
// The full walk costs one facet request plus ~pages: 22,346 postings at 100 a
// page is ~230 requests, ~75 postings per request — cheaper per posting than
// every HTML platform here and behind only the big JSON list APIs.
//
// # Bounds
//
// Each slice ends on its own short page. [pageRepeatGuard] ends a slice that
// ignores its offset, [amazonMaxPagesPerSlice] holds unconditionally at the
// window edge, and a posting seen twice — pages reorder between requests, and
// a job recategorized mid-crawl can appear in two slices — is yielded once,
// deduplicated by canonical URL exactly like the iCIMS walk.
func Amazon(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://www.amazon.jobs/en/search.json?base_query=&offset=0&result_limit=1&facets[]=business_category&facets[]=normalized_country_code
	return func(yield func(*internal.JobPosting, error) bool) {
		facetsDoc, err := fetchJSON[amazonSearch](ctx, httpClient, amazonPlatform, company, jsonRequest{
			URL: amazonSearchURL(0, 1, "", "", true),
		})
		if err != nil {
			yield(nil, err)

			return
		}

		if facetsDoc.Error != "" {
			yield(nil, fmt.Errorf("unexpected error from %s for company %q: %s", amazonPlatform, company, facetsDoc.Error))

			return
		}

		var (
			categories = amazonFacets(facetsDoc.Facets.BusinessCategory)
			countries  = amazonFacets(facetsDoc.Facets.Country)
			seen       = make(map[string]bool)
		)

		if len(categories) == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from %s for company %q: no business_category facet, so the board cannot be partitioned inside its %d-row result window", amazonPlatform, company, amazonMaxWindow))

			return
		}

		for _, category := range categories {
			if category.Count == 0 {
				continue
			}

			if category.Count < amazonMaxWindow {
				if !amazonWalkSlice(ctx, httpClient, company, category.Name, "", seen, yield) {
					return
				}

				continue
			}

			// A category the size of the window cannot be walked whole; fetch
			// its own country facet and walk the intersections.
			subDoc, err := fetchJSON[amazonSearch](ctx, httpClient, amazonPlatform, company, jsonRequest{
				URL: amazonSearchURL(0, 1, category.Name, "", true),
			})
			if err != nil {
				yield(nil, err)

				return
			}

			for _, country := range amazonFacets(subDoc.Facets.Country) {
				if country.Count == 0 {
					continue
				}

				if !amazonWalkSlice(ctx, httpClient, company, category.Name, country.Name, seen, yield) {
					return
				}
			}
		}

		// The partition check, after the postings so a drifting facet still
		// contributes everything it served. Both facets are computed over the
		// same result set in the same response, so a mismatch means some
		// postings carry a value outside one facet's buckets and were never
		// inside any slice this walk asked for.
		if categoryTotal, countryTotal := amazonFacetTotal(categories), amazonFacetTotal(countries); countryTotal > 0 && categoryTotal != countryTotal {
			yield(nil, fmt.Errorf("the %s business_category facet no longer partitions the board for company %q: its buckets sum to %d postings against the country facet's %d, so the difference was never reachable by any slice of this walk", amazonPlatform, company, categoryTotal, countryTotal))
		}
	}
}

// amazonWalkSlice pages through one filtered slice of the board, reporting
// false when the consumer stopped or a request failed.
func amazonWalkSlice(ctx context.Context, httpClient *http.Client, company, category, country string, seen map[string]bool, yield func(*internal.JobPosting, error) bool) bool {
	var (
		guard  pageRepeatGuard
		offset int
	)

	for page := 0; page < amazonMaxPagesPerSlice; page++ {
		if ctx.Err() != nil {
			return yieldError(yield, ctx.Err())
		}

		// The server rejects any request reaching past the window outright, so
		// the last page before the edge asks only for what the window allows.
		limit := min(amazonPageSize, amazonMaxWindow-offset)
		if limit <= 0 {
			return true
		}

		doc, err := fetchJSON[amazonSearch](ctx, httpClient, amazonPlatform, company, jsonRequest{
			URL: amazonSearchURL(offset, limit, category, country, false),
		})
		if err != nil {
			return yieldError(yield, err)
		}

		if doc.Error != "" {
			return yieldError(yield, fmt.Errorf("unexpected error from %s for company %q at offset %d of business_category %q: %s", amazonPlatform, company, offset, category, doc.Error))
		}

		if len(doc.Jobs) == 0 {
			return true
		}

		ids := make([]string, 0, len(doc.Jobs))
		for _, job := range doc.Jobs {
			ids = append(ids, job.JobPath)
		}

		if guard.repeated(ids) {
			return true
		}

		for _, job := range doc.Jobs {
			path := strings.TrimSpace(job.JobPath)
			title := strings.TrimSpace(job.Title)

			if !strings.HasPrefix(path, "/") || title == "" {
				continue
			}

			postingURL := "https://" + amazonHost + path

			if seen[postingURL] {
				continue
			}

			seen[postingURL] = true

			location := strings.TrimSpace(job.NormalizedLocation)
			if location == "" {
				location = strings.TrimSpace(job.Location)
			}

			if location == "" {
				location = "unknown"
			}

			posting := &internal.JobPosting{
				Company:  company,
				URL:      postingURL,
				Title:    title,
				Location: location,

				Department: strings.TrimSpace(job.JobCategory),
				Team:       strings.TrimSpace(job.JobFamily),
				ExternalID: strings.TrimSpace(job.IDIcims),
				PostedAt:   amazonTime(job.PostedDate),
				Source: internal.PostingSource{
					Platform: amazonPlatform,
					Key:      company,
				},
			}

			if employment, ok := internal.NormalizeEmploymentType(job.JobScheduleType); ok {
				posting.EmploymentType = employment
			}

			if !yield(posting, nil) {
				return false
			}
		}

		if len(doc.Jobs) < limit {
			return true
		}

		offset += len(doc.Jobs)
	}

	return true
}

// yieldError reports an error through yield and always returns false, purely so
// the walk above can end a slice and the whole crawl in one expression.
func yieldError(yield func(*internal.JobPosting, error) bool, err error) bool {
	yield(nil, err)

	return false
}
