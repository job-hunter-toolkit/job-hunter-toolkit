package services

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin(multiJobsFunc(Lever, LeverCompanies))
}

var LeverCompanies = []string{
	"15five",
	"360learning",
	"3pillarglobal",
	"activecampaign",
	"aircall",
	"anchorage",
	"animocabrands",
	"anomali",
	"aquabyte",
	"articulate",
	"atlassian",
	"backerkit",
	"balbix",
	"bazaarvoice",
	"benchsci",
	"bighealth",
	"binance",
	"boxlunch",
	"brevo",
	"brightwheel",
	"brilliant",
	"brillio-2",
	"carbonhealth",
	"celerion",
	"celestia",
	"certik",
	"clari",
	"clerky",
	"clicktime",
	"cloudwalk",
	"coalfire",
	"cobaltrobotics",
	"coupa",
	"cred",
	"cyderes",
	"d2l",
	"deputy",
	"despegar",
	"dlocal",
	"dnb",
	"docebo",
	"edpuzzle",
	"enablecomp",
	"entrata",
	"epifi",
	"fitbod",
	"fond",
	"freeletics",
	"freshworks",
	"fullscript",
	"geocomply-2",
	"gettyimages",
	"girlswhocode",
	"gynger",
	"highspot",
	"hottopic",
	"houzz",
	"immuta",
	"immutable",
	"includedhealth",
	"increase",
	"insomniacookies",
	"instructure",
	"instrument",
	"issuu",
	"jamcity",
	"jitxinc",
	"jobandtalent",
	"kariusdx",
	"kinsta",
	"kpmgnz",
	"kraken",
	"lalamove",
	"ledger",
	"lever",
	"linkedin",
	"logrocket",
	"LuxorTechnology",
	"lyrahealth",
	"masterycharter",
	"medium",
	"meesho",
	"meili",
	"metabase",
	"mindtickle",
	"mirror",
	"netflix",
	"nielsen",
	"ninjavan",
	"nium",
	"offchainlabs",
	"okendo",
	"omnisend",
	"outreach",
	"palantir",
	"paytm",
	"peakgames",
	"people-ai",
	"perforce",
	"pipedrive",
	"placemakr",
	"plaid",
	"poki",
	"polleverywhere",
	"ppfa",
	"provi",
	"qonto",
	"quartzy",
	"quizlet-2",
	"rackspace",
	"replate",
	"researchgate",
	"restaurant365",
	"ro",
	"rocketship",
	"rover",
	"royalambulance",
	"sambatv",
	"saviynt",
	"scaleway",
	"secureframe",
	"sensortower",
	"sierraclub",
	"signal",
	"skipscooters",
	"smarsh",
	"sonatype",
	"sophos",
	"superpedestrian",
	"surfshark",
	"swissborg",
	"swordhealth",
	"SymmetrySystems",
	"symplicity",
	"sysdig",
	"tala",
	"teller",
	"theathletic",
	"thinkahead",
	"tiket",
	"tinybird",
	"toptal",
	"translifeline",
	"trendyol",
	"trustly",
	"vergegenomics",
	"viget",
	"voleon",
	"voodoo",
	"voro",
	"warmly",
	"wealthfront",
	"wealthsimple",
	"weride",
	"whoop",
	"wintermute-trading",
	"woven-by-toyota",
	"zeta",
	"zilliz",
	"zimperium",
	"zoox",
}

type leverJobs []struct {
	//AdditionalPlain string `json:"additionalPlain"`
	//Additional      string `json:"additional"`
	Categories struct {
		//Commitment string `json:"commitment"`
		//Department string `json:"department"`
		//Level      string `json:"level"`
		Location string `json:"location"`
		//Team       string `json:"team"`
	} `json:"categories"`
	//CreatedAt        int64  `json:"createdAt"`
	//DescriptionPlain string `json:"descriptionPlain"`
	//Description      string `json:"description"`
	//ID               string `json:"id"`
	//Lists            []struct {
	//	Text    string `json:"text"`
	//	Content string `json:"content"`
	//} `json:"lists"`
	Text      string `json:"text"`
	HostedURL string `json:"hostedUrl"`

	// SalaryRange is a genuine structured field, but sparsely populated: it is
	// absent entirely on many boards and filled on only a fraction of postings
	// where present.
	SalaryRange *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	} `json:"salaryRange"`
	//ApplyURL   string `json:"applyUrl"`
}

// leverPage fetches a single page of Lever postings. It exists so the response
// body is closed when the page is done rather than accumulating one open body
// per page for the lifetime of the whole crawl.
func leverPage(ctx context.Context, httpClient *http.Client, company string, limit, skip int) (leverJobs, error) {
	query := url.Values{
		"mode":  {"json"},
		"limit": {strconv.Itoa(limit)},
		"skip":  {strconv.Itoa(skip)},
	}

	doc, err := fetchJSON[leverJobs](ctx, httpClient, "Lever", company, jsonRequest{
		URL: "https://api.lever.co/v0/postings/" + company + "?" + query.Encode(),
	})
	if err != nil {
		return nil, err
	}

	return *doc, nil
}

// Lever returns the job postings for the given company using the provided HTTP client.
func Lever(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// The v0 version of lever has been largely deprecated and is not returning results for many companies.
	// There's a v1 which requires authentication.
	//
	// https://hire.lever.co/developer/documentation#introduction
	return func(yield func(*internal.JobPosting, error) bool) {
		const limit = 100

		skip := 0

		for {
			doc, err := leverPage(ctx, httpClient, company, limit, skip)
			if err != nil {
				yield(nil, err)
				return
			}

			if len(doc) == 0 {
				break
			}

			for _, item := range doc {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				var (
					url         = strings.TrimSpace(strings.Replace(item.HostedURL, "http://", "https://", -1))
					titleStr    = strings.TrimSpace(item.Text)
					locationStr = cmp.Or(strings.TrimSpace(item.Categories.Location), "unknown/remote")
				)

				if !yield(&internal.JobPosting{
					Company:      company,
					URL:          url,
					Title:        titleStr,
					Location:     locationStr,
					Compensation: leverCompensation(item.SalaryRange),
				}, nil) {
					return
				}
			}

			if len(doc) < limit {
				break
			}

			skip += limit
		}
	}
}

// leverIntervals maps Lever's salary interval onto [internal.Period].
//
// Lever spells these as "per-year-salary" style slugs, so the mapping matches on
// the unit contained in the value rather than on an exact string.
var leverIntervals = map[string]internal.Period{
	"year":  internal.PeriodYear,
	"month": internal.PeriodMonth,
	"week":  internal.PeriodWeek,
	"day":   internal.PeriodDay,
	"hour":  internal.PeriodHour,
}

// leverCompensation builds a pay range from Lever's salaryRange, returning nil
// when the posting publishes none.
func leverCompensation(salary *struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Currency string  `json:"currency"`
	Interval string  `json:"interval"`
}) *internal.Compensation {
	if salary == nil || (salary.Min <= 0 && salary.Max <= 0) {
		return nil
	}

	comp := &internal.Compensation{
		Min:        salary.Min,
		Max:        salary.Max,
		Currency:   strings.ToUpper(strings.TrimSpace(salary.Currency)),
		Provenance: internal.ProvenanceEmployer,
	}

	interval := strings.ToLower(salary.Interval)

	for unit, period := range leverIntervals {
		if strings.Contains(interval, unit) {
			comp.Period = period

			break
		}
	}

	return comp
}
