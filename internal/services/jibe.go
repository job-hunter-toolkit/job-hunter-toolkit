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
		)

		for {
			apiResp, err := jibePage(ctx, httpClient, company, baseURL, page)
			if err != nil {
				yield(nil, err)
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

			if len(apiResp.Jobs) < jibePageSize {
				return
			}

			page++
		}
	}
}
