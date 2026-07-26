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
	"careers.kbr.com",
	"careers.mccain.com",
	"careers.molsoncoors.com",
	"careers.oreillyauto.com",
	"careers.pentair.com",
	"careers.ppg.com",
	"careers.southwestair.com",
	"careers.united.com",
	"careers.zimmerbiomet.com",
	"jobs.bechtel.com",
	"talent.lowes.com",
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
// extra byte, no new host. They are typed `any` rather than string because,
// unlike the four fields above them, no response from a real tenant has been
// decoded here to confirm their JSON type — this container cannot reach a Phenom
// site. An `any` cannot fail a decode, and one failed decode here loses the
// whole page and therefore the whole tenant, which is exactly how a fixed type
// for Jibe's "meta_data" silently disabled nine large employers.
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
				urlStr := strings.TrimSpace(job.ApplyURL)

				if urlStr == "" && job.JobID != "" {
					// Some tenants handle applications on the Phenom site
					// itself rather than an external ATS, so "applyUrl" is
					// absent for them entirely; it is not merely empty on
					// individual postings. The job detail route only looks
					// at the ID segment of the path (verified live: a wrong
					// or missing title slug still resolves), so this always
					// reaches the posting.
					urlStr = fmt.Sprintf("https://%s/us/en/job/%s", company, job.JobID)
				}

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
