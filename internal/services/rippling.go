package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

func init() {
	registerBuiltin("rippling", multiJobsFunc(Rippling, RipplingCompanies))
}

var RipplingCompanies = []string{
	"aalo-atomics",
	"adden-energy",
	"adelaide",
	"aeris-communications-inc",
	"airbox",
	"armada-careers",
	"arrow-electric",
	"aspenview",
	"bamboo-health-careers",
	"bastion-careers",
	"bettercomp",
	"bfgcareers",
	"bonsairoboticsmain",
	"brevian-careers",
	"campspot",
	"careers-ucfs",
	"cbts",
	"centil-jobs",
	"chess",
	"clearstar",
	"closinglock",
	"commandlink",
	"community-phone-careers",
	"compass-coffee",
	"concerthealthcareers",
	"covetcareers",
	"cozey",
	"cpp-careers",
	"createmusicgroup",
	"crenndininggroup",
	"cruciallogics",
	"curiosity-current-openings",
	"daloopa",
	"dialogue-en",
	"dpirecruiting",
	"droneshield",
	"earthbanc_careers",
	"echeloncareers",
	"eridu-ai",
	"eventeny",
	"fabric",
	"fellers",
	"flighthouse",
	"forterra",
	"foundation-robotics",
	"fountane",
	"framework",
	"futuri-careers",
	"galileo",
	"get-engaged",
	"gga",
	"go-forth",
	"hammerspace",
	"harborcompliance",
	"hercules-careers",
	"hockeystack-careers",
	"howard-financial",
	"hungerhub",
	"hunterstrategy",
	"icp",
	"intelliguard",
	"intersection",
	"jome",
	"just-appraised-jobs",
	"kellen",
	"legitscript-careers",
	"lighthouseai-careers",
	"liquiddeath",
	"livewire",
	"luupli",
	"mapped",
	"meundies-recruiting",
	"mission-action",
	"misterpexpress",
	"mozn-ai",
	"nerdio-careers",
	"nihao",
	"nstxl",
	"orderful",
	"orthoatlanta",
	"orthopediatrics",
	"owlet",
	"padel-haus",
	"panthera-dental-inc",
	"parentsquare",
	"partnerco",
	"pathos",
	"pgcareers",
	"plantiblefoods",
	"plume-network",
	"poggio",
	"prepory",
	"proformance-builder-solutions",
	"redbaycoffeecareers",
	"reverb-careers",
	"rhythms",
	"rightcrowdcareers",
	"royalbrassandhose",
	"serviceupcareers",
	"sheerid",
	"signwarehouse",
	"smash",
	"sockettelecomcareers",
	"stark-carpet-corp",
	"stellar-virtual",
	"sumersports",
	"summerset",
	"swag",
	"tempo",
	"thriveglobal",
	"tilledcareers",
	"tissuehealthplus",
	"tixr",
	"toyota-of-olympia",
	"tpstalent",
	"turtlebox",
	"ugsolutions",
	"unily",
	"usare",
	"vectorhealthai",
	"ventriclehealth",
	"vimachem",
	"whistic-careers",
	"widewail",
	"windwalker-group",
	"xypncareers",
	"zadentech",
	"zevra",
}

// RipplingJobData represents the structure of the job data from Rippling
type RipplingJobData struct {
	Props struct {
		PageProps struct {
			DehydratedState struct {
				Queries []struct {
					State struct {
						Data struct {
							Items []struct {
								ID         string `json:"id"`
								Name       string `json:"name"`
								URL        string `json:"url"`
								Department struct {
									Name string `json:"name"`
								} `json:"department"`
								Locations []struct {
									Name          string `json:"name"`
									Country       string `json:"country"`
									CountryCode   string `json:"countryCode"`
									State         string `json:"state"`
									StateCode     string `json:"stateCode"`
									City          string `json:"city"`
									WorkplaceType string `json:"workplaceType"`
								} `json:"locations"`
								Language string `json:"language"`
							} `json:"items"`
						} `json:"data"`
					} `json:"state"`
					QueryKey []interface{} `json:"queryKey"`
				} `json:"queries"`
			} `json:"dehydratedState"`
		} `json:"pageProps"`
	} `json:"props"`
}

func Rippling(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			baseURL = fmt.Sprintf("https://ats.rippling.com/%s/jobs", company)
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
		if err != nil {
			yield(nil, err)
			return
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			yield(nil, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			yield(nil, fmt.Errorf("unexpected status code from Rippling for %q: %s", company, resp.Status))
			return
		}

		// Parse HTML to find the script with the job data
		doc, err := html.Parse(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("failed to parse HTML: %w", err))
			return
		}

		// Extract the JSON data from the script tag with id="__NEXT_DATA__"
		scriptContent := extractScriptContent(doc)
		if scriptContent == "" {
			yield(nil, fmt.Errorf("could not find __NEXT_DATA__ script in the HTML"))
			return
		}

		// Parse the JSON data
		var jobData RipplingJobData
		if err := json.Unmarshal([]byte(scriptContent), &jobData); err != nil {
			yield(nil, fmt.Errorf("failed to parse job data JSON: %w", err))
			return
		}

		// Find the query that contains the job listings
		for _, query := range jobData.Props.PageProps.DehydratedState.Queries {
			// Check if this query contains job posts
			if len(query.QueryKey) >= 3 {
				// Try to convert the second element to a string
				if companySlug, ok := query.QueryKey[1].(string); ok && companySlug == company {
					// Check if the third element suggests this is a job posts query
					if jobPostsKey, ok := query.QueryKey[2].(string); ok && jobPostsKey == "job-posts" {
						// Extract and yield job postings
						for _, job := range query.State.Data.Items {
							if ctx.Err() != nil {
								yield(nil, ctx.Err())
								return
							}

							// Construct location string
							var locations []string
							for _, loc := range job.Locations {
								location := loc.Name
								if location == "" {
									parts := []string{}
									if loc.City != "" {
										parts = append(parts, loc.City)
									}
									if loc.State != "" {
										parts = append(parts, loc.State)
									}
									if loc.Country != "" {
										parts = append(parts, loc.Country)
									}
									if loc.WorkplaceType == "REMOTE" {
										parts = append(parts, "Remote")
									}
									location = strings.Join(parts, ", ")
								}
								locations = append(locations, location)
							}

							if !yield(&internal.JobPosting{
								Company:  company,
								URL:      job.URL,
								Title:    job.Name,
								Location: strings.Join(locations, "; "),
							}, nil) {
								return
							}
						}
						// Found and processed the job listings query, no need to continue
						return
					}
				}
			}
		}

		// A board with no openings ships a page whose dehydrated query set is
		// empty, which is indistinguishable from a board that has jobs but whose
		// query we failed to recognise. Reporting an error here marked seven real,
		// reachable boards as broken in a health check, so an empty query set is
		// treated as "no postings right now" instead.
		//
		// A page that carries queries but none of them job posts is still a
		// genuine parse failure worth reporting.
		if len(jobData.Props.PageProps.DehydratedState.Queries) > 0 {
			yield(nil, fmt.Errorf("could not find job listings data for Rippling company %q", company))
		}
	}
}

// extractScriptContent finds the script tag with id="__NEXT_DATA__" and returns its text content
func extractScriptContent(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "script" {
		var id string
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == "__NEXT_DATA__" {
				id = attr.Val
				break
			}
		}
		if id == "__NEXT_DATA__" {
			if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
				return n.FirstChild.Data
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if content := extractScriptContent(c); content != "" {
			return content
		}
	}

	return ""
}
