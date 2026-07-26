package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin("jibe", multiJobsFunc(Jibe, JibeCompanies))
}

// jibePageSize is the number of postings requested per page.
const jibePageSize = 100

// jibeMaxPages bounds how many pages a single Jibe tenant may be asked for.
//
// "totalCount" and [pageRepeatGuard] are the real stops; this is the backstop
// for a tenant that reports no usable total and keeps serving different full
// pages forever, which is how the unbounded loop this replaces ran up 5,001
// requests and 500,001 duplicate postings against a stub in 0.8s. At 100
// postings per page it still allows 200,000 postings from one company, well
// above the largest Jibe boards here (FedEx, Costco, Marriott), so reaching it
// means the board is misbehaving and the adapter says so rather than crawling
// on.
const jibeMaxPages = 2000

var JibeCompanies = []string{
	"84lumber",
	"alaskaair",
	"amedisys",
	"ascension",
	"bjc",
	"brightspring",
	"carenewengland",
	"casella",
	"celanese",
	"chsli",
	"commonspirit",
	"conehealth",
	"costco",
	"cubesmart",
	"delawarenorth",
	"discounttire",
	"dollargeneral",
	"dunhamssports",
	"eagleview",
	"einstein",
	"emory",
	"exeloncorp",
	"farmersinsurance",
	"fedex",
	"footlocker",
	"generalmills",
	"githubinc",
	"gnc",
	"heb",
	"jcpenney",
	"marriott",
	"medstarhealth",
	"mercy",
	"merlinentertainments",
	"mountsinai",
	"naturalgrocers",
	"nfiindustries",
	"noodles",
	"novanthealth",
	"obhs",
	"ohsu",
	"orlandohealth",
	"paychex",
	"penfed",
	"pennentertainment",
	"pepsico",
	"petsmart",
	"piedmont",
	"redlobster",
	"rei",
	"riteaid",
	"rockefelleruniversity",
	"rush",
	"sheetz",
	"siteone",
	"sixflags",
	"sprouts",
	"statefarm",
	"stjude",
	"suncoastcreditunion",
	"sutterhealth",
	"thecheesecakefactory",
	"towerhealth",
	"ucla",
	"uhs",
	"ulta",
	"umms",
	"unitypoint",
	"wakemed",
	"wendys",
	"xanterra",
}

// jibeJobs is the subset of Jibe's job search response that this adapter uses.
//
// The full response carries a large amount of additional metadata (Google Jobs
// derived info, facet lists, language counts). It is deliberately not modelled
// here: some fields are polymorphic across tenants, the top-level "meta_data"
// is an object for some companies and a bare `false` for others; so decoding
// them into fixed Go types made every such tenant fail to decode.
type jibeJobs struct {
	Jobs []struct {
		Data struct {
			// Jibe is one of the few platforms that publishes pay as structured
			// numbers, and it populates them often, measured at 69 of 100
			// PetSmart postings. Frequency is an enum like "HOURLY"; currency is
			// frequently empty even when the amounts are present.
			SalaryMin       float64 `json:"salary_min_value"`
			SalaryMax       float64 `json:"salary_max_value"`
			SalaryCurrency  string  `json:"salary_currency"`
			SalaryFrequency string  `json:"salary_frequency"`

			Title        string `json:"title"`
			ApplyURL     string `json:"apply_url"`
			FullLocation string `json:"full_location"`
		} `json:"data,omitempty"`
	} `json:"jobs"`
	TotalCount int `json:"totalCount"`
}

// jibeFrequencies maps Jibe's pay frequency enum onto [internal.Period].
var jibeFrequencies = map[string]internal.Period{
	"HOURLY":  internal.PeriodHour,
	"DAILY":   internal.PeriodDay,
	"WEEKLY":  internal.PeriodWeek,
	"MONTHLY": internal.PeriodMonth,
	"YEARLY":  internal.PeriodYear,
	"ANNUAL":  internal.PeriodYear,
}

// jibeCompensation builds a pay range from Jibe's salary fields, returning nil
// when the tenant published no amounts.
//
// Jibe sends explicit zeros rather than omitting the fields, so a zero range
// means "not disclosed" and must not become a posting that claims to pay nothing.
func jibeCompensation(minValue, maxValue float64, currency, frequency string) *internal.Compensation {
	if minValue <= 0 && maxValue <= 0 {
		return nil
	}

	return &internal.Compensation{
		Min:        minValue,
		Max:        maxValue,
		Currency:   strings.ToUpper(strings.TrimSpace(currency)),
		Period:     jibeFrequencies[strings.ToUpper(strings.TrimSpace(frequency))],
		Provenance: internal.ProvenanceEmployer,
	}
}

// jibePage fetches a single page of Jibe postings. It exists so the response
// body is closed when the page is done rather than accumulating one open body
// per page for the lifetime of the whole crawl.
func jibePage(ctx context.Context, httpClient *http.Client, company, baseURL string, page int) (*jibeJobs, error) {
	query := url.Values{
		"page":  {strconv.Itoa(page)},
		"limit": {strconv.Itoa(jibePageSize)},
	}

	return fetchJSON[jibeJobs](ctx, httpClient, "Jibe", company, jsonRequest{
		URL: baseURL + "?" + query.Encode(),
	})
}

// Jibe returns job postings from Jibe's API for a given company. It's unclear to me where this
// API is documented now, but it seems like it's still available even after the ICIIMS acquisition.
//
// https://www.icims.com/company/newsroom/icims-acquires-jibe-to-provide-employers-best-in-class-candidate-engagement-and-recruitment-marketing-capabilities/
func Jibe(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			baseURL = fmt.Sprintf("https://%s.jibeapply.com/api/jobs", company)
			page    = 1
			pages   pageRepeatGuard
			fetched int
		)

		for range jibeMaxPages {
			apiResp, err := jibePage(ctx, httpClient, company, baseURL, page)
			if err != nil {
				yield(nil, err)
				return
			}

			if len(apiResp.Jobs) == 0 {
				return
			}

			ids := make([]string, 0, len(apiResp.Jobs))
			for _, item := range apiResp.Jobs {
				ids = append(ids, item.Data.ApplyURL)
			}

			// Checked before anything is yielded, so a tenant that ignores
			// "page" costs one wasted request rather than an endless stream of
			// duplicates.
			if pages.repeated(ids) {
				return
			}

			for _, item := range apiResp.Jobs {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				var (
					url         = strings.TrimSpace(strings.Replace(item.Data.ApplyURL, "http://", "https://", -1))
					titleStr    = strings.TrimSpace(item.Data.Title)
					locationStr = strings.TrimSpace(item.Data.FullLocation)
				)

				if url != "" && titleStr != "" && locationStr != "" {
					if !yield(&internal.JobPosting{
						Company:      company,
						URL:          url,
						Title:        titleStr,
						Location:     locationStr,
						Compensation: jibeCompensation(item.Data.SalaryMin, item.Data.SalaryMax, item.Data.SalaryCurrency, item.Data.SalaryFrequency),
					}, nil) {
						return
					}
				}
			}

			// Counted in the units totalCount uses, postings the search matched,
			// not postings this adapter considered complete enough to yield.
			fetched += len(apiResp.Jobs)

			// totalCount is the only authority Jibe gives for "there is nothing
			// after this page", and it was decoded but never read until this
			// loop was bounded.
			//
			// It is trusted only when it exceeds a single page: a totalCount
			// equal to the page size is indistinguishable from a per-page count,
			// and reading one as the other would cap every large tenant at 100
			// postings, the silent-truncation failure this project has been
			// burned by before. Giving up that one case costs a single extra
			// request on a board whose posting count is an exact multiple of the
			// page size, which the short-page check below then ends.
			if apiResp.TotalCount > jibePageSize && fetched >= apiResp.TotalCount {
				return
			}

			if len(apiResp.Jobs) < jibePageSize {
				return
			}

			page++
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Jibe for company %q: the board was still serving full pages after %d pages of %d", company, jibeMaxPages, jibePageSize))
	}
}
