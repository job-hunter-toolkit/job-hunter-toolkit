package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

// ripplingPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const ripplingPlatform = "rippling"

func init() {
	registerBuiltin(ripplingPlatform, multiJobsFunc(Rippling, RipplingCompanies))
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
							Items []ripplingJobPost `json:"items"`

							// TotalPages and TotalItems are the board's own count
							// of what it has, and they ride in the same payload
							// the adapter already parses.
							//
							// They were never decoded, and the embedded query is
							// page 0 at a page size of 20, so every Rippling
							// board with more than twenty entries was silently
							// truncated to its first twenty. Measured on
							// 2026-07-28: 22 of the 99 registered boards that
							// returned anything returned exactly 20, the shape
							// of a cap rather than of a coincidence.
							// ats.rippling.com/aspenview/jobs says
							// "totalItems": 70 in the very response that this
							// adapter read 20 postings out of.
							TotalPages int `json:"totalPages"`
							TotalItems int `json:"totalItems"`
						} `json:"data"`
					} `json:"state"`
					QueryKey []interface{} `json:"queryKey"`
				} `json:"queries"`
			} `json:"dehydratedState"`
		} `json:"pageProps"`
	} `json:"props"`
}

// ripplingMaxPages bounds how many pages one board may be asked for.
//
// totalPages comes from the board, so it is a number a third party controls;
// [pageRepeatGuard] catches a board that ignores ?page= and this bounds one that
// reports an absurd count. At the platform's page size of 20 it still allows
// 2,000 postings from a single tenant, far above the largest observed (70).
const ripplingMaxPages = 100

// ripplingJobPost is one opening in a board's dehydrated query state.
//
// Department and the per-location workplaceType have been decoded since this
// adapter was written. The department was never read at all, and workplaceType
// was consulted at exactly one place, to append the literal word "Remote" to a
// location string, and only in the branch where the location had no name of its
// own; for most postings the board's structured workplace answer was decoded and
// then thrown away.
type ripplingJobPost struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Department struct {
		Name string `json:"name"`
	} `json:"department"`
	Locations []ripplingLocation `json:"locations"`
	Language  string             `json:"language"`
}

// ripplingLocation is one site a posting is offered at.
type ripplingLocation struct {
	Name          string `json:"name"`
	Country       string `json:"country"`
	CountryCode   string `json:"countryCode"`
	State         string `json:"state"`
	StateCode     string `json:"stateCode"`
	City          string `json:"city"`
	WorkplaceType string `json:"workplaceType"`
}

// ripplingWorkplaceType returns the workplace answer a posting's locations
// agree on, or unknown when they do not.
//
// Rippling attaches workplaceType per site, so a posting offered both at a
// remote site and at an office has two different answers and no single true one.
// Picking the first would make the value depend on the order the board happened
// to serialise its locations in, which is the kind of coin-flip data that is
// worse than an empty field: a `--workplace-type remote` user can see an absent
// value, but cannot see a wrong one.
func ripplingWorkplaceType(locations []ripplingLocation) internal.WorkplaceType {
	resolved := internal.WorkplaceTypeUnknown

	for _, location := range locations {
		workplace, ok := internal.NormalizeWorkplaceType(location.WorkplaceType)
		if !ok {
			continue
		}

		if resolved != internal.WorkplaceTypeUnknown && resolved != workplace {
			return internal.WorkplaceTypeUnknown
		}

		resolved = workplace
	}

	return resolved
}

func Rippling(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			items  []ripplingJobPost
			guard  pageRepeatGuard
			pages  = 1
			merged = make(map[string]int)
		)

		for page := 0; page < pages && page < ripplingMaxPages; page++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			posts, total, err := ripplingPage(ctx, httpClient, company, page)
			if err != nil {
				yield(nil, err)
				return
			}

			if len(posts) == 0 {
				break
			}

			ids := make([]string, 0, len(posts))
			for _, post := range posts {
				ids = append(ids, post.ID)
			}

			if guard.repeated(ids) {
				break
			}

			// One opening is repeated once per site it is offered at, with the
			// same id and the same URL each time and a "locations" array holding
			// only that one site. Yielded per entry, those become postings
			// sharing a URL and [internal.Dedupe] keeps only the first, so an
			// opening in seven places was published in one of them: measured on
			// aspenview, 20 entries carried 7 distinct openings.
			for _, post := range posts {
				if at, seen := merged[post.URL]; seen && post.URL != "" {
					items[at].Locations = append(items[at].Locations, post.Locations...)

					continue
				}

				merged[post.URL] = len(items)
				items = append(items, post)
			}

			if page == 0 && total > 1 {
				pages = total
			}
		}

		for _, job := range items {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			posting := &internal.JobPosting{
				Company:  company,
				URL:      job.URL,
				Title:    job.Name,
				Location: ripplingLocationText(job.Locations),

				Department:    strings.TrimSpace(job.Department.Name),
				WorkplaceType: ripplingWorkplaceType(job.Locations),
				ExternalID:    job.ID,
				Source:        internal.PostingSource{Platform: ripplingPlatform, Key: company},
			}

			// Recorded only in the affirmative. "Not remote" is not the same
			// statement as "must be in an office", and a false Remote here would
			// switch off the location-text fallback in
			// [internal.JobPosting.IsRemote] for every hybrid posting on the
			// platform.
			if posting.WorkplaceType == internal.WorkplaceTypeRemote {
				remote := true
				posting.Remote = &remote
			}

			if !yield(posting, nil) {
				return
			}
		}
	}
}

// ripplingLocationText renders every site an opening is offered at.
func ripplingLocationText(locations []ripplingLocation) string {
	names := make([]string, 0, len(locations))

	for _, location := range locations {
		name := location.Name

		if name == "" {
			parts := make([]string, 0, 4)

			for _, part := range []string{location.City, location.State, location.Country} {
				if part != "" {
					parts = append(parts, part)
				}
			}

			if location.WorkplaceType == "REMOTE" {
				parts = append(parts, "Remote")
			}

			name = strings.Join(parts, ", ")
		}

		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return strings.Join(names, "; ")
}

// ripplingPage fetches one page of a board, returning its openings and the
// number of pages the board says it has.
//
// It exists as its own function so the response body is closed when the page is
// done rather than accumulating one open body per page for the whole crawl, per
// docs/adding-a-source.md.
func ripplingPage(ctx context.Context, httpClient *http.Client, company string, page int) ([]ripplingJobPost, int, error) {
	// The board's own front end asks for pages this way, and the embedded query
	// key names the parameter it was rendered for.
	pageURL := fmt.Sprintf("https://ats.rippling.com/%s/jobs?page=%d", company, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("building request for Rippling company %q: %w", company, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetching Rippling company %q: %w", company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("unexpected status code from Rippling for %q: %s", company, resp.Status)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing HTML for Rippling company %q: %w", company, err)
	}

	// The JSON data lives in the script tag with id="__NEXT_DATA__".
	scriptContent := extractScriptContent(doc)
	if scriptContent == "" {
		return nil, 0, fmt.Errorf("could not find __NEXT_DATA__ script for Rippling company %q", company)
	}

	var jobData RipplingJobData
	if err := json.Unmarshal([]byte(scriptContent), &jobData); err != nil {
		return nil, 0, fmt.Errorf("parsing job data JSON for Rippling company %q: %w", company, err)
	}

	for _, query := range jobData.Props.PageProps.DehydratedState.Queries {
		if len(query.QueryKey) < 3 {
			continue
		}

		companySlug, ok := query.QueryKey[1].(string)
		if !ok || companySlug != company {
			continue
		}

		if jobPostsKey, ok := query.QueryKey[2].(string); !ok || jobPostsKey != "job-posts" {
			continue
		}

		return query.State.Data.Items, query.State.Data.TotalPages, nil
	}

	// A board with no openings ships a page whose dehydrated query set is empty,
	// which is indistinguishable from a board that has jobs but whose query we
	// failed to recognise. Reporting an error here marked seven real, reachable
	// boards as broken in a health check, so an empty query set is treated as "no
	// postings right now" instead.
	//
	// A page that carries queries but none of them job posts is still a genuine
	// parse failure worth reporting.
	if len(jobData.Props.PageProps.DehydratedState.Queries) > 0 {
		return nil, 0, fmt.Errorf("could not find job listings data for Rippling company %q", company)
	}

	return nil, 0, nil
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
