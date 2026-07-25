package services

import (
	"context"
	"net/http"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin(multiJobsFunc(Workable, WorkableCompanies))
}

var WorkableCompanies = []string{
	"ae-perkins",
	"ahv-international",
	"bardel-entertainment",
	"bartlett-and-co-dot-llc",
	"basetwo",
	"bci-brands",
	"beyond-next-ventures",
	"biopharma-consulting-jad-group",
	"butterflymx",
	"centorrino-technologies",
	"datacom1",
	"detroitlabs",
	"ebcfinancialgroup",
	"enfos-inc",
	"enrollhere",
	"equus-software",
	"fte-factory-advisors",
	"g-mass",
	"gearup2success-1",
	"golftec1",
	"gomining",
	"indiancreekschool",
	"io-global",
	"jobgether",
	"keylane",
	"moodle",
	"netguru",
	"northstrat",
	"oceansxyz",
	"ohara-corporation",
	"pearlabyss-europe",
	"persado",
	"prophix",
	"refloor",
	"reversinglabs",
	"rezilient",
	"rwinvest",
	"seismic",
	"serenity-mental-health-centers",
	"shift-online",
	"sigmadefense",
	"silver-hills-bakery",
	"simple-mills-9",
	"slp",
	"smartcommerce",
	"spacemachines",
	"stio",
	"the-brydon-group",
	"the-desire-company",
	"thesignalgroup",
	"titan-environmental-solutions-inc",
	"trailofbits",
	"vix-technology",
	"workstate",
	"zipdev",
	"zyte",
}

type workableResp struct {
	Total   int `json:"total"`
	Results []struct {
		ID        int    `json:"id"`
		Shortcode string `json:"shortcode"`
		Title     string `json:"title"`
		Remote    bool   `json:"remote"`
		Location  struct {
			Country     string `json:"country"`
			CountryCode string `json:"countryCode"`
			City        string `json:"city"`
			Region      string `json:"region"`
		} `json:"location"`
		Locations []struct {
			Country     string `json:"country"`
			CountryCode string `json:"countryCode"`
			City        string `json:"city"`
			Region      string `json:"region"`
			Hidden      bool   `json:"hidden"`
		} `json:"locations"`
		State          string    `json:"state"`
		IsInternal     bool      `json:"isInternal"`
		Code           string    `json:"code"`
		Published      time.Time `json:"published"`
		Type           string    `json:"type,omitempty"`
		Language       string    `json:"language"`
		Department     []string  `json:"department"`
		AccountUID     string    `json:"accountUid"`
		ApprovalStatus string    `json:"approvalStatus"`
		Workplace      string    `json:"workplace"`
	} `json:"results"`
}

// Workable returns all of the job postings for a given company, or an
// error if there was a problem making the request or parsing the response.
func Workable(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://apply.workable.com/$company/#jobs
	// https://apply.workable.com/api/v3/accounts/$company/jobs
	// https://apply.workable.com/$company/j/$job_id
	return func(yield func(*internal.JobPosting, error) bool) {
		// Note: to include job description, simply add the "?content=true" URL param to the request.
		doc, err := fetchJSON[workableResp](ctx, httpClient, "Workable", company, jsonRequest{
			Method: http.MethodPost,
			URL:    "https://apply.workable.com/api/v3/accounts/" + company + "/jobs",
		})
		if err != nil {
			yield(nil, err)

			return
		}

		for _, job := range doc.Results {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			jobPosting := &internal.JobPosting{
				Title:    job.Title,
				Company:  company,
				Location: job.Location.Country,
				URL:      "https://apply.workable.com/" + company + "/jobs/" + job.Shortcode + "/",
			}

			if !yield(jobPosting, nil) {
				return
			}
		}
	}
}
