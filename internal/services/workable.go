package services

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// workablePlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const workablePlatform = "workable"

func init() {
	registerBuiltin(workablePlatform, multiJobsFunc(Workable, WorkableCompanies))
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
	"european-dynamics",
	"famoco",
	"flosum",
	"fte-factory-advisors",
	"g-mass",
	"gearup2success-1",
	"golftec1",
	"gomining",
	"iita",
	"imachines",
	"indiancreekschool",
	"io-global",
	"jobgether",
	"jones-knowles-ritchie",
	"keylane",
	"kreyco",
	"liberty-mutual-canada",
	"moodle",
	"netguru",
	"northstrat",
	"oceansxyz",
	"ohara-corporation",
	"pearlabyss-europe",
	"persado",
	"propeller",
	"prophix",
	"refloor",
	"reversinglabs",
	"rezilient",
	"rwinvest",
	"seeq",
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
	"supportyourapp",
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
	Jobs []workableJob `json:"jobs"`
}

// workableJob is one opening in the widget feed.
type workableJob struct {
	Shortcode string `json:"shortcode"`
	Title     string `json:"title"`

	// Telecommuting is Workable's own structured remote flag. It has been
	// decoded since this adapter was written and spent only on appending the
	// word "Remote" to a location string, which meant `--remote` had to rediscover
	// it by looking for that word in free text.
	Telecommuting bool                    `json:"telecommuting"`
	URL           string                  `json:"url"`
	Locations     []workableLocationEntry `json:"locations"`
}

// workableLocationEntry is one site a posting is offered at.
type workableLocationEntry struct {
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
	Region      string `json:"region"`
	Hidden      bool   `json:"hidden"`
}

// Workable returns all of the job postings for a given company, or an
// error if there was a problem making the request or parsing the response.
func Workable(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://apply.workable.com/$company/#jobs
	// https://apply.workable.com/api/v1/widget/accounts/$company
	// https://apply.workable.com/j/$job_id
	return func(yield func(*internal.JobPosting, error) bool) {
		// Workable's v3 search endpoint enforces an IP-wide daily quota and can
		// return Retry-After values longer than the crawl's entire time budget.
		// The public v1 widget endpoint powers the careers page, returns the same
		// open jobs in one smaller GET, and is not subject to that quota.
		doc, err := fetchJSON[workableResp](ctx, httpClient, "Workable", company, jsonRequest{
			URL: "https://apply.workable.com/api/v1/widget/accounts/" + company,
		})
		if err != nil {
			yield(nil, err)

			return
		}

		for _, job := range doc.Jobs {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			location := workableLocation(job.Locations, job.Telecommuting)
			url := job.URL
			if url == "" && job.Shortcode != "" {
				url = "https://apply.workable.com/j/" + job.Shortcode
			}

			if strings.TrimSpace(job.Title) == "" || url == "" {
				continue
			}

			jobPosting := &internal.JobPosting{
				Title:    strings.TrimSpace(job.Title),
				Company:  company,
				Location: location,
				URL:      url,

				ExternalID: job.Shortcode,
				Source:     internal.PostingSource{Platform: workablePlatform, Key: company},
			}

			// Carried only when the employer set the flag. Workable publishes no
			// hybrid/onsite distinction here, so telecommuting=false says "not
			// fully remote" and nothing more; turning that into onsite would
			// invent an office requirement the employer never stated, and
			// setting Remote to false would switch off the location-text
			// fallback in [internal.JobPosting.IsRemote] for the whole platform.
			if job.Telecommuting {
				remote := true

				jobPosting.Remote = &remote
				jobPosting.WorkplaceType = internal.WorkplaceTypeRemote
			}

			if !yield(jobPosting, nil) {
				return
			}
		}
	}
}

func workableLocation(locations []workableLocationEntry, remote bool) string {
	names := make([]string, 0, len(locations)+1)

	for _, location := range locations {
		if location.Hidden {
			continue
		}

		parts := []string{location.City, location.Region, location.Country}
		parts = slices.DeleteFunc(parts, func(part string) bool {
			return strings.TrimSpace(part) == ""
		})

		if name := strings.Join(parts, ", "); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	if remote {
		names = append(names, "Remote")
	}

	if len(names) == 0 {
		return "unknown"
	}

	return strings.Join(names, "; ")
}
