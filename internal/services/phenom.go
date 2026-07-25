package services

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin(multiJobsFuncNamed(Phenom, PhenomCompanies, phenomCompanyName))
}

// phenomPageSize is the number of postings requested per page.
const phenomPageSize = 100

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
		Jobs []struct {
			Title     string `json:"title"`
			CityState string `json:"cityState"`
			Location  string `json:"location"`
			ApplyURL  string `json:"applyUrl"`
			JobID     string `json:"jobId"`
		} `json:"jobs"`
	} `json:"data"`
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
		companyName := phenomCompanyName(company)

		for from := 0; ; from += phenomPageSize {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			page, err := phenomPage(ctx, httpClient, company, from)
			if err != nil {
				yield(nil, err)
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

				if !yield(&internal.JobPosting{
					Company:  companyName,
					URL:      urlStr,
					Title:    titleStr,
					Location: locationStr,
				}, nil) {
					return
				}
			}

			if len(page.Data.Jobs) < phenomPageSize {
				return
			}
		}
	}
}
