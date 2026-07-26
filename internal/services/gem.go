package services

import (
	"cmp"
	"context"
	"fmt"
	"net/http"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin("gem", multiJobsFunc(Gem, GemCompanies))
}

var GemCompanies = []string{
	"agora",
	"apartment-list",
	"bilt",
	"bluesky",
	"breadcrumb-ai",
	"cloudanix-com",
	"cloudraft",
	"conductorai",
	"deep-infra",
	"distributed-spectrum",
	"doordash",
	"eliza",
	"engineering--codified",
	"fanpierlabs-com",
	"felix",
	"fetch",
	"function-health",
	"gc-ai",
	"gem",
	"gem-oats",
	"genesisdigital-co",
	"getro",
	"inception",
	"index-exchange",
	"letter-ai",
	"lumalabs-ai",
	"mission",
	"modular",
	"myriad-technology",
	"nominal",
	"ocient-inc-",
	"plixai",
	"pogo-recruiting",
	"portal-ai",
	"prahsys-com",
	"quo",
	"retool",
	"rivia",
	"roamless",
	"sequencing",
	"silkline",
	"soundhound",
	"ssg",
	"system-two-security",
	"the-boring-company",
	"theburntapp-com",
	"thunder",
	"tropic",
	"up-labs",
	"veho-technologies",
	"wonolo",
}

type gemJobs struct {
	Data struct {
		PublicBrandingTheme     any `json:"publicBrandingTheme"`
		OatsExternalJobPostings struct {
			JobPostings []struct {
				ID                      string `json:"id"`
				Title                   string `json:"title"`
				DescriptionHTML         string `json:"descriptionHtml"`
				ExtID                   string `json:"extId"`
				StartDateTs             any    `json:"startDateTs"`
				FirstPublishedTsSec     int    `json:"firstPublishedTsSec"`
				CompanyLogo             any    `json:"companyLogo"`
				CompanyURL              any    `json:"companyUrl"`
				IsApplicationFormHidden bool   `json:"isApplicationFormHidden"`
				Locations               []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					City       string `json:"city"`
					IsoCountry string `json:"isoCountry"`
					IsRemote   bool   `json:"isRemote"`
					ExtID      string `json:"extId"`
					Typename   string `json:"__typename"`
				} `json:"locations"`
				Job struct {
					ID             string `json:"id"`
					LocationType   string `json:"locationType"`
					EmploymentType string `json:"employmentType"`
					RequisitionID  string `json:"requisitionId"`
					Department     struct {
						ID       string `json:"id"`
						Name     string `json:"name"`
						ExtID    string `json:"extId"`
						Typename string `json:"__typename"`
					} `json:"department"`
					Locations []struct {
						ID         string `json:"id"`
						Name       string `json:"name"`
						City       string `json:"city"`
						IsoCountry string `json:"isoCountry"`
						IsRemote   bool   `json:"isRemote"`
						ExtID      string `json:"extId"`
						Typename   string `json:"__typename"`
					} `json:"locations"`
					Typename string `json:"__typename"`
				} `json:"job"`
				Typename string `json:"__typename"`
			} `json:"jobPostings"`
			Typename string `json:"__typename"`
		} `json:"oatsExternalJobPostings"`
		OatsExternalJobPostingsFilters []struct {
			Type        string `json:"type"`
			DisplayName string `json:"displayName"`
			RawValue    string `json:"rawValue"`
			Value       string `json:"value"`
			Count       int    `json:"count"`
			Typename    string `json:"__typename"`
		} `json:"oatsExternalJobPostingsFilters"`
		JobBoardExternal struct {
			ID              string `json:"id"`
			TeamDisplayName string `json:"teamDisplayName"`
			DescriptionHTML string `json:"descriptionHtml"`
			PageTitle       string `json:"pageTitle"`
			Typename        string `json:"__typename"`
		} `json:"jobBoardExternal"`
	} `json:"data,omitempty"`
}

//	curl -X POST 'https://jobs.gem.com/api/public/graphql' \
//	 -H 'Content-Type: application/json' \
//	 -d '{
//	   "query": "query JobPostingInfo($boardId: String!) { oatsExternalJobPostings(boardId: $boardId) { jobPostings { id extId title companyUrl locations { name city isoCountry } } } }",
//	   "variables": { "boardId": "bluesky" }
//	 }'
func Gem(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			baseURL = "https://jobs.gem.com/api/public/graphql"

			// GraphQL returns exactly the fields the query selects, so a field
			// this adapter reads but does not ask for is silently empty.
			//
			// "extId" is the segment every posting URL is built from and it was
			// missing from this selection set, so every posting on a board came
			// back as https://jobs.gem.com/<company>/ — one URL for the whole
			// company. internal.Dedupe keys on the URL, so each of the 51 Gem
			// sources collapsed to a single posting (measured: 3 postings in, 1
			// out). Adding one word to this string restores the other ~50x.
			query = `{"query":"query JobPostingInfo($boardId: String!) { oatsExternalJobPostings(boardId: $boardId) { jobPostings { id extId title companyUrl locations { name city isoCountry } } } }","variables":{"boardId":"%s"}}`
		)

		doc, err := fetchJSON[gemJobs](ctx, httpClient, "Gem", company, jsonRequest{
			Method: http.MethodPost,
			URL:    baseURL,
			Body:   fmt.Sprintf(query, company),
		})
		if err != nil {
			yield(nil, err)

			return
		}

		for _, item := range doc.Data.OatsExternalJobPostings.JobPostings {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			// extId is the segment Gem's own public board uses. Falling back to
			// the internal id keeps postings distinguishable if a tenant ever
			// publishes one without an extId: a link that may not resolve is
			// recoverable, whereas identical URLs are deleted outright by
			// internal.Dedupe and the posting is never seen again.
			id := cmp.Or(item.ExtID, item.ID)
			if id == "" {
				continue
			}

			url := fmt.Sprintf("https://jobs.gem.com/%s/%s", company, id)

			job := &internal.JobPosting{
				Company: company,
				Title:   item.Title,
				URL:     url,
			}

			if len(item.Locations) > 0 {
				job.Location = item.Locations[0].Name + ", " + item.Locations[0].City + ", " + item.Locations[0].IsoCountry
			}

			if !yield(job, nil) {
				return
			}
		}
	}
}
