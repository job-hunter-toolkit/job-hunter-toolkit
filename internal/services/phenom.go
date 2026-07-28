package services

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// phenomPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const phenomPlatform = "phenom"

func init() {
	registerBuiltin(phenomPlatform, multiJobsFuncNamed(Phenom, PhenomCompanies, phenomCompanyName))
}

// phenomPageSize is the number of postings requested per page.
const phenomPageSize = 100

// phenomMaxPages bounds how many pages a single Phenom tenant may be asked for.
//
// Phenom is the likeliest platform here to need it: the adapter re-requests the
// *same* server-rendered search-results page with a different "from", so a
// tenant whose SSR ignores "from" serves identical jobs forever. Replayed
// against a stub that behaves that way, this loop issued 5,001 requests and
// yielded 500,001 duplicate postings in 0.9s and stopped only because the
// consumer gave up. [pageRepeatGuard] catches the identical-page case; this is
// the backstop for a tenant that varies its pages without ever running out. At
// 100 postings per page it allows 50,000 postings from one company, far above
// any tenant in [PhenomCompanies].
const phenomMaxPages = 500

// PhenomCompanies holds the hostnames of Phenom People career sites this
// project crawls.
//
// Unlike most other ATSes covered here, Phenom tenants share no common
// subdomain convention, some serve from "careers.<company>.com", others
// from "talent.<company>.com" or "jobs.<company>.com"; so each entry is a
// full hostname rather than a short slug.
var PhenomCompanies = []string{
	"careers.conagrabrands.com",
	"careers.dupont.com",
	"careers.humana.com",
	"careers.itw.com",
	"careers.mccain.com",
	"careers.molsoncoors.com",
	"careers.oreillyauto.com",
	"careers.pentair.com",
	"careers.ppg.com",
	"careers.southwestair.com",
	"careers.united.com",
	"careers.zimmerbiomet.com",
	"jobs.bechtel.com",
}

// phenomSearchResults is the subset of a Phenom People search-results page's
// embedded search payload that this adapter uses.
//
// Phenom career sites are almost entirely client-side rendered, and their own
// client-side search action ("refineSearch" in the site's bundled JS) answers
// a plain HTTP client with facet counts only, never the job list. The initial
// page load, however, embeds the first batch of results directly in a
// "phApp.ddo.eagerLoadRefineSearch" JSON object so the page has content before
// its JavaScript runs. This adapter reads that embedded object, keyed by
// requesting successive "from" offsets on the same page, rather than the
// client's own search API.
type phenomSearchResults struct {
	Data struct {
		Jobs []phenomJob `json:"jobs"`
	} `json:"data"`
}

// phenomJob is one opening in the embedded search payload.
//
// PostedDate, Type and Category arrive in the same blob the adapter already
// downloads and parses, so reading them costs nothing: no extra request, no
// extra byte, no new host.
//
// All three were JSON strings in a first page from each of the 15 tenants in
// [PhenomCompanies] (1,398 postings, decoded live), so the `any` is no longer
// there because the shape is unknown. It stays because one failed decode loses
// the whole page and therefore the whole tenant, and Phenom is a per-tenant
// template rather than one API: a single employer switching "type" to an object
// would take its whole board down. That is exactly how a fixed type for Jibe's
// "meta_data" silently disabled nine large employers. An `any` cannot fail a
// decode, and [anyText] narrows it at the point of use.
type phenomJob struct {
	Title     string `json:"title"`
	CityState string `json:"cityState"`
	Location  string `json:"location"`
	ApplyURL  string `json:"applyUrl"`
	JobID     string `json:"jobId"`

	PostedDate any `json:"postedDate"`
	Type       any `json:"type"`
	Category   any `json:"category"`
}

// phenomDateLayouts are the timestamp spellings accepted for "postedDate".
//
// Only unambiguous ones. A slash-separated date is deliberately absent: 03/04
// is the third of April to half the world and the fourth of March to the other
// half, and there is no field in the payload that says which tenant means which.
// Guessing would put a date a month wrong into [internal.Filter.PostedSince],
// where nothing downstream could ever notice; leaving it empty is visible.
var phenomDateLayouts = []string{
	time.RFC3339,

	// RFC 3339 but with a colonless zone offset, which is what Phenom tenants
	// actually publish: "2026-07-16T00:00:00.000+0000". time.RFC3339 rejects
	// that offset outright, so without this a posting reaches
	// [internal.Filter.PostedSince] undated.
	//
	// This is not a rare spelling to tolerate, it is the only one observed:
	// every postedDate on a first page from all 15 tenants in [PhenomCompanies]
	// was written this way, so until this layout existed the platform's whole
	// PostedAt column was empty. Go's parser accepts a fractional second the
	// layout does not mention, so this one entry covers the ".000" and
	// bare-seconds spellings both.
	"2006-01-02T15:04:05-0700",

	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// phenomPostedAt converts a posting's "postedDate" to UTC, reporting false when
// it is missing or in a spelling this does not know.
func phenomPostedAt(raw any) (time.Time, bool) {
	text := anyText(raw)
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range phenomDateLayouts {
		if posted, err := time.Parse(layout, text); err == nil {
			return posted.UTC(), true
		}
	}

	return time.Time{}, false
}

// phenomEagerLoadMarker precedes the embedded search payload in a
// search-results page's HTML: `phApp.ddo = {...,"eagerLoadRefineSearch":{...},...};`.
const phenomEagerLoadMarker = `"eagerLoadRefineSearch":`

// phenomPage fetches one page of a Phenom company's search results.
//
// Split out so the response body is closed per page rather than accumulating
// one open body per page for the lifetime of the crawl.
func phenomPage(ctx context.Context, httpClient *http.Client, company string, from int) (*phenomSearchResults, error) {
	url := fmt.Sprintf("https://%s/us/en/search-results?from=%d&size=%d", company, from, phenomPageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Phenom company %q: %w", company, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Phenom for company %q: %w", company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from Phenom for company %q: %s", company, resp.Status)
	}

	// The response is an HTML page with the search payload embedded in a
	// script tag, not a JSON document on its own, so the relevant object has
	// to be located inside it first. json.Decoder stops at the end of the
	// first complete JSON value, so pointing it at the object's opening brace
	// is enough, the JS and HTML that follows never needs to be parsed.
	body := &strings.Builder{}
	if _, err := io.Copy(body, resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response from Phenom for company %q: %w", company, err)
	}

	idx := strings.Index(body.String(), phenomEagerLoadMarker)
	if idx == -1 {
		return nil, fmt.Errorf("failed to find search results in Phenom page for company %q: page layout may have changed", company)
	}

	var page phenomSearchResults

	dec := json.NewDecoder(strings.NewReader(body.String()[idx+len(phenomEagerLoadMarker):]))
	if err := dec.Decode(&page); err != nil {
		return nil, fmt.Errorf("failed to decode search results from Phenom for company %q: %w", company, err)
	}

	return &page, nil
}

// phenomCompanyName derives a short display name from a Phenom tenant
// hostname, by taking the label before the TLD: given
// "careers.southwestair.com" it returns "southwestair".
//
// Unlike Workday, whose tenant subdomain IS the company slug, a Phenom
// hostname's first label is a product subdomain ("careers", "talent",
// "jobs"), so the company name sits one label further in.
func phenomCompanyName(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}

	return labels[len(labels)-2]
}

// phenomPostingURL returns the link for one posting on a Phenom tenant.
//
// It is the tenant's OWN job-detail route, built from "jobId", and deliberately
// not the posting's "applyUrl".
//
// # Why applyUrl is wrong here
//
// A Phenom career site is a front end. For most tenants the application itself
// is handled by a different ATS, and "applyUrl" points at that other system --
// measured 2026-07-28 by reading the first page of all 14 tenants in
// [PhenomCompanies]: 9 published a Workday URL, 2 a SuccessFactors URL, 1 a
// Taleo URL, and only 2 (mccain, molsoncoors) published no applyUrl at all. So
// yielding applyUrl made this adapter emit another platform's URL for 12 of 14
// tenants, on postings whose [internal.PostingSource] says "phenom".
//
// That is not merely untidy, it defeats deduplication. [internal.Dedupe] keys on
// URL, and a Phenom applyUrl onto Workday is the Workday posting URL with
// "/apply" appended, so the two routes to one opening never collapse. It cost
// 5,103 postings on Lowe's (see deletedDoubleCountRoutes in
// double_count_test.go) and, measured the same day, 1,556 more on KBR, which is
// registered on both platforms right now: 1,556 of careers.kbr.com's 1,558
// distinct URLs were exactly a registered Workday URL plus "/apply", and zero
// matched it as written.
//
// The tenant's own route is the honest answer and needs no extra request: the
// job-detail page reads only the ID segment of the path, and all 14 tenants
// published a non-empty, per-posting-distinct jobId on every row of a 100-row
// page. Six were fetched end to end and every canonical URL answered 200 with
// the posting's own title rendered.
//
// applyUrl remains the fallback so that a tenant which ever omits jobId keeps a
// link rather than being dropped, which is what this project's contract that
// every posting carries an openable URL requires.
func phenomPostingURL(company string, job phenomJob) string {
	if id := strings.TrimSpace(job.JobID); id != "" {
		return fmt.Sprintf("https://%s/us/en/job/%s", company, id)
	}

	return strings.TrimSpace(job.ApplyURL)
}

// Phenom returns the job postings for a company hosted on Phenom People.
//
// company is a Phenom tenant's hostname (e.g. "careers.southwestair.com"),
// not a short slug, see [PhenomCompanies].
func Phenom(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			companyName = phenomCompanyName(company)
			pages       pageRepeatGuard
		)

		for n := range phenomMaxPages {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			from := n * phenomPageSize

			page, err := phenomPage(ctx, httpClient, company, from)
			if err != nil {
				yield(nil, err)
				return
			}

			if len(page.Data.Jobs) == 0 {
				return
			}

			ids := make([]string, 0, len(page.Data.Jobs))
			for _, job := range page.Data.Jobs {
				// Both, because tenants that render applications on the Phenom
				// site itself omit applyUrl entirely for every posting, which
				// would make each page fingerprint as the same empty list.
				ids = append(ids, job.ApplyURL, job.JobID)
			}

			// Checked before anything is yielded, so a tenant whose search page
			// ignores "from" costs one wasted request rather than an endless
			// stream of duplicates.
			if pages.repeated(ids) {
				return
			}

			for _, job := range page.Data.Jobs {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				titleStr := strings.TrimSpace(job.Title)
				urlStr := phenomPostingURL(company, job)

				if titleStr == "" || urlStr == "" {
					continue
				}

				locationStr := cmp.Or(strings.TrimSpace(job.CityState), strings.TrimSpace(job.Location))
				if locationStr == "" {
					locationStr = "unknown/remote"
				}

				posting := &internal.JobPosting{
					Company:  companyName,
					URL:      urlStr,
					Title:    titleStr,
					Location: locationStr,

					// Phenom's "category" is the job family a tenant files the
					// posting under, which is what this project calls a
					// department; it is the field a person means by
					// `--department engineering` on these boards.
					Department: anyText(job.Category),
					ExternalID: job.JobID,
					Source:     internal.PostingSource{Platform: phenomPlatform, Key: company},
				}

				if employment, ok := internal.NormalizeEmploymentType(anyText(job.Type)); ok {
					posting.EmploymentType = employment
				}

				if posted, ok := phenomPostedAt(job.PostedDate); ok {
					posting.PostedAt = posted
				}

				if !yield(posting, nil) {
					return
				}
			}

			if len(page.Data.Jobs) < phenomPageSize {
				return
			}
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Phenom for company %q: the search was still serving full pages after %d pages of %d", company, phenomMaxPages, phenomPageSize))
	}
}
