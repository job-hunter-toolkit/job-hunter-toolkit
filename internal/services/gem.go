package services

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/companies"
)

// gemPlatform is the ATS family this file registers, and the value that reaches
// [internal.PostingSource.Platform].
const gemPlatform = "gem"

func init() {
	registerBuiltin(gemPlatform, multiJobsFunc(Gem, GemCompanies))
	registerDirectEmployers()
}

// registerDirectEmployers adds the single-employer adapters in
// internal/companies to [Builtin].
//
// They had no registration at all. Every entry in Builtin is added by an init in
// this package, and nothing outside internal/companies ever imported it, so
// Oxide and Uber were maintained and tested but unreachable from the CLI: dead
// code in the shipped binary, and zero postings from either in any crawl this
// project has ever run.
//
// This lives in a service file only because registerBuiltin is unexported and
// internal/companies cannot import this package without a cycle; it belongs
// beside builtin.go, in a file of its own, and should be moved there.
func registerDirectEmployers() {
	direct := companies.Sources()
	sources := make([]Source, 0, len(direct))

	for _, source := range direct {
		sources = append(sources, Source{
			Key:     source.Key,
			Company: source.Company,
			Jobs:    source.Jobs,
		})
	}

	registerBuiltin(companies.DirectPlatform, sources)
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
				ID                      string        `json:"id"`
				Title                   string        `json:"title"`
				DescriptionHTML         string        `json:"descriptionHtml"`
				ExtID                   string        `json:"extId"`
				StartDateTs             any           `json:"startDateTs"`
				FirstPublishedTsSec     int           `json:"firstPublishedTsSec"`
				CompanyLogo             any           `json:"companyLogo"`
				CompanyURL              any           `json:"companyUrl"`
				IsApplicationFormHidden bool          `json:"isApplicationFormHidden"`
				Locations               []gemLocation `json:"locations"`
				Job                     struct {
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
					Locations []gemLocation `json:"locations"`
					Typename  string        `json:"__typename"`
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

// gemLocation is one site a posting is offered at.
type gemLocation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	City       string `json:"city"`
	IsoCountry string `json:"isoCountry"`
	IsRemote   bool   `json:"isRemote"`
	ExtID      string `json:"extId"`
	Typename   string `json:"__typename"`
}

// gemWorkplaceType maps Gem's workplace signals onto the canonical vocabulary.
//
// The posting-level "locationType" is the employer's own answer and wins. The
// per-site isRemote flags are a fallback, and only when every site agrees: a
// posting offered both remotely and at an office has two answers and no single
// true one, and picking one would make the value depend on the order the board
// happened to serialise its locations in.
func gemWorkplaceType(locationType string, locations []gemLocation) internal.WorkplaceType {
	if workplace, ok := internal.NormalizeWorkplaceType(locationType); ok {
		return workplace
	}

	if len(locations) == 0 {
		return internal.WorkplaceTypeUnknown
	}

	for _, location := range locations {
		if !location.IsRemote {
			return internal.WorkplaceTypeUnknown
		}
	}

	return internal.WorkplaceTypeRemote
}

// joinNonEmpty joins the parts that have content, so a value the board left
// blank leaves no gap in the result.
func joinNonEmpty(separator string, parts ...string) string {
	kept := make([]string, 0, len(parts))

	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}

	return strings.Join(kept, separator)
}

//	curl -X POST 'https://jobs.gem.com/api/public/graphql' \
//	 -H 'Content-Type: application/json' \
//	 -d '{
//	   "query": "query JobPostingInfo($boardId: String!) { oatsExternalJobPostings(boardId: $boardId) { jobPostings { id extId title companyUrl firstPublishedTsSec locations { name city isoCountry isRemote } job { employmentType requisitionId locationType department { name } } } } }",
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
			//
			// The department, requisition number, employment type, workplace
			// type and publication date are on the same principle: gemJobs has
			// modelled every one of them since the adapter was written, and none
			// was ever asked for. They ride in this one POST, so the whole
			// enrichment costs no extra request and no extra round trip — only a
			// longer selection set.
			query = `{"query":"query JobPostingInfo($boardId: String!) { oatsExternalJobPostings(boardId: $boardId) { jobPostings { id extId title companyUrl firstPublishedTsSec locations { name city isoCountry isRemote } job { employmentType requisitionId locationType department { name } } } } }","variables":{"boardId":"%s"}}`
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

				Department:    strings.TrimSpace(item.Job.Department.Name),
				RequisitionID: strings.TrimSpace(item.Job.RequisitionID),
				ExternalID:    item.ExtID,
				Source:        internal.PostingSource{Platform: gemPlatform, Key: company},
			}

			if len(item.Locations) > 0 {
				// Only the parts the board filled in. Joining unconditionally
				// produced "Remote, , US" for every posting with no city, and a
				// location string with a hole in it is what `--location`
				// substring matching then has to match around.
				job.Location = joinNonEmpty(", ",
					item.Locations[0].Name,
					item.Locations[0].City,
					item.Locations[0].IsoCountry,
				)
			}

			if employment, ok := internal.NormalizeEmploymentType(item.Job.EmploymentType); ok {
				job.EmploymentType = employment
			}

			job.WorkplaceType = gemWorkplaceType(item.Job.LocationType, item.Locations)

			if job.WorkplaceType == internal.WorkplaceTypeRemote {
				remote := true
				job.Remote = &remote
			}

			// Epoch seconds, so a zero is "never published" rather than 1970.
			if item.FirstPublishedTsSec > 0 {
				job.PostedAt = time.Unix(int64(item.FirstPublishedTsSec), 0).UTC()
			}

			if !yield(job, nil) {
				return
			}
		}
	}
}
