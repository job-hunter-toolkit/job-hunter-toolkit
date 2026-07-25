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
	registerBuiltin("smartrecruiters", multiJobsFunc(SmartRecruiters, SmartRecruitersCompanies))
}

var SmartRecruitersCompanies = []string{
	"Accor",
	"Alorica",
	"Armis",
	"ASOS",
	"Auto1",
	"AveryDennison",
	"BoschGroup",
	"ChristianBrothersAutomotive",
	"Chubb2",
	"CityFibre",
	"Colliers",
	"Continental",
	"CrunchFitness",
	"DeliveryHero",
	"DeloitteNetherlands",
	"Dominos",
	"Equinox",
	"Expeditors",
	"FosterFarms",
	"FrasersGroup",
	"Gameloft",
	"GEHealthcare2",
	"GEICO",
	"HaltonHealthcare1",
	"ifs1",
	"Justworks",
	"JYSK",
	"KimberlyClark",
	"KittitasValleyHealthcare",
	"LVMH",
	"McDonaldsCorporation",
	"Nine",
	"northwesternmedicine",
	"NorwegianCruiseLine",
	"optiv",
	"ORICPharmaceuticals",
	"PaloAltoNetworks2",
	"PharmaCannis",
	"Primark",
	"PublicStorage",
	"RaisingCanes",
	"SanaCommerce",
	"ServiceNow",
	"Sixt",
	"Sodexo",
	"SonicAutomotive",
	"TheNielsenCompany",
	"Tipico",
	"TTEC",
	"TurnerConstruction",
	"visa",
	"Wise",
	"wtw",
	"Xplor",
}

type smartRecruitersJobs struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	TotalFound int `json:"totalFound"`
	Content    []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Location struct {
			City    string `json:"city"`
			Region  string `json:"region"`
			Country string `json:"country"`
		} `json:"location"`
	} `json:"content"`
}

// smartRecruitersPage fetches one page of SmartRecruiters postings.
func smartRecruitersPage(ctx context.Context, httpClient *http.Client, company string, offset int) (*smartRecruitersJobs, error) {
	query := url.Values{"offset": {strconv.Itoa(offset)}}

	return fetchJSON[smartRecruitersJobs](ctx, httpClient, "SmartRecruiters", company, jsonRequest{
		URL: "https://api.smartrecruiters.com/v1/companies/" + company + "/postings?" + query.Encode(),
	})
}

// SmartRecruiters returns the job postings for a company hosted on
// SmartRecruiters.
//
// Note that this API answers HTTP 200 for any company name, real or not, with
// totalFound of zero. A zero-posting result therefore does not distinguish "not
// hiring" from "no such tenant", which matters when verifying a new entry.
func SmartRecruiters(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		for offset := 0; ; {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			doc, err := smartRecruitersPage(ctx, httpClient, company, offset)
			if err != nil {
				yield(nil, err)

				return
			}

			// An empty page ends pagination. Checked before advancing the offset
			// because a zero-length page would otherwise leave the offset
			// unchanged and loop forever.
			if len(doc.Content) == 0 {
				return
			}

			for _, item := range doc.Content {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())

					return
				}

				location := strings.Join([]string{
					item.Location.City,
					item.Location.Region,
					item.Location.Country,
				}, ",")

				if !yield(&internal.JobPosting{
					Company:  company,
					URL:      fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", company, item.ID),
					Title:    strings.TrimSpace(item.Name),
					Location: location,
				}, nil) {
					return
				}
			}

			offset += len(doc.Content)

			if offset >= doc.TotalFound {
				return
			}
		}
	}
}
