package services

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// leverPlatform is the platform name this file registers under, shared with the
// [internal.PostingSource] every posting carries so the two cannot drift apart.
const leverPlatform = "lever"

func init() {
	registerBuiltin(leverPlatform, multiJobsFunc(Lever, LeverCompanies))
}

// leverMaxPages bounds how many pages a single Lever tenant may be asked for.
//
// Lever publishes no total, so [pageRepeatGuard] is the primary stop for a board
// that ignores "skip"; this is the backstop for the case a fingerprint cannot
// see, a board that keeps serving *different* full pages forever. At 100
// postings per page it allows 50,000 postings from one company, an order of
// magnitude above the largest Lever board observed, so reaching it means
// something is wrong and the adapter says so rather than crawling on.
const leverMaxPages = 500

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

// leverJobs is one page of a Lever tenant's public postings.
//
// Most of these fields used to sit here commented out, which is a record that
// somebody saw them in a real response and chose not to decode them. They ride
// in the same `?mode=json&limit=100&skip=N` page the adapter already downloads,
// so decoding them adds no request and no byte to a crawl of 161 Lever sources.
//
// Two documented fields are still not decoded, on purpose. `lists` (the bulleted
// sections of the body) and `country` have nowhere to go: [internal.JobPosting]
// has no country field, and the lists array would multiply this struct's
// per-page allocation for a body the posting does not keep.
type leverJobs []struct {
	// AdditionalPlain is the closing block of the posting body, which is where
	// Lever boards conventionally put the pay-transparency paragraph, so it is
	// scanned for a pay range alongside the opening body.
	AdditionalPlain string `json:"additionalPlain"`

	Categories struct {
		// Commitment is Lever's employment type, spelled "Fulltime",
		// "Full-time", "Part-time", "Contract", "Intern" or "unspecified"
		// depending on the tenant. Normalized rather than stored raw.
		Commitment string `json:"commitment"`

		// Department is the coarse org unit and Team the finer one inside it,
		// which is the split [internal.JobPosting.Department] documents.
		Department string `json:"department"`
		Team       string `json:"team"`

		// Level is the board's own ladder label, "Senior" or "L5". It lands in
		// the free-string Seniority field rather than an enum, because levelling
		// is a per-employer ladder.
		Level string `json:"level"`

		Location string `json:"location"`
	} `json:"categories"`

	// CreatedAt is epoch milliseconds, not seconds and not a string.
	CreatedAt int64 `json:"createdAt"`

	// DescriptionPlain is the opening block of the posting body as text. As on
	// Ashby it is decoded only to run pay extraction over prose that is already
	// on the wire, and is not kept on the posting.
	DescriptionPlain string `json:"descriptionPlain"`

	// ID is Lever's posting identifier, a UUID that survives the title changes
	// that move hostedUrl.
	ID string `json:"id"`

	// WorkplaceType is Lever's structured answer: "unspecified", "on-site",
	// "remote" or "hybrid". "unspecified" is deliberately left unrecognised by
	// [internal.NormalizeWorkplaceType].
	WorkplaceType string `json:"workplaceType"`

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

		var (
			pages pageRepeatGuard
			skip  int
		)

		for range leverMaxPages {
			doc, err := leverPage(ctx, httpClient, company, limit, skip)
			if err != nil {
				yield(nil, err)
				return
			}

			if len(doc) == 0 {
				return
			}

			ids := make([]string, 0, len(doc))
			for _, item := range doc {
				ids = append(ids, item.HostedURL)
			}

			// Checked before anything is yielded, so a board that ignores "skip"
			// costs one wasted request rather than an endless stream of
			// duplicates.
			if pages.repeated(ids) {
				return
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

				comp := leverCompensation(item.SalaryRange)

				// salaryRange is a real structured field but sparsely filled, and
				// the body it would have been written in is already downloaded.
				// MoreTrustedThan keeps the two apart: prose fills an empty field
				// and never overwrites the employer's own numbers, and carries
				// [internal.ProvenanceDescription] when it does.
				if fromDescription := internal.ParseCompensationFromDescription(leverDescription(item.DescriptionPlain, item.AdditionalPlain)); fromDescription.MoreTrustedThan(comp) {
					comp = fromDescription
				}

				posting := &internal.JobPosting{
					Company:      company,
					URL:          url,
					Title:        titleStr,
					Location:     locationStr,
					Compensation: comp,
					Department:   strings.TrimSpace(item.Categories.Department),
					Team:         strings.TrimSpace(item.Categories.Team),
					Seniority:    strings.TrimSpace(item.Categories.Level),
					PostedAt:     leverCreatedAt(item.CreatedAt),
					ExternalID:   strings.TrimSpace(item.ID),
					Source: internal.PostingSource{
						Platform: leverPlatform,
						Key:      company,
					},
				}

				if employment, ok := internal.NormalizeEmploymentType(item.Categories.Commitment); ok {
					posting.EmploymentType = employment
				}

				// When Lever states a workplace type it also settles the remote
				// question, and the board's own answer beats
				// [internal.JobPosting.IsRemote] guessing from location text.
				// This does change `--remote` for Lever: a posting the board
				// marks on-site or hybrid no longer matches on the strength of
				// the word "Remote" appearing in its location. That is the point
				// — hybrid is not remote, and "unspecified" still leaves the flag
				// nil so the heuristic keeps working where the board says nothing.
				if workplace, ok := internal.NormalizeWorkplaceType(item.WorkplaceType); ok {
					remote := workplace == internal.WorkplaceTypeRemote

					posting.WorkplaceType = workplace
					posting.Remote = &remote
				}

				if !yield(posting, nil) {
					return
				}
			}

			if len(doc) < limit {
				return
			}

			skip += limit
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Lever for company %q: the board was still serving full pages after %d pages of %d", company, leverMaxPages, limit))
	}
}

// leverCreatedAt converts Lever's createdAt into UTC, returning the zero time
// when the board published none.
//
// Lever publishes epoch *milliseconds*. Reading them as seconds would date a
// posting created on 2026-01-01 to the year 57971 and quietly satisfy every
// [internal.Filter.PostedSince] query ever asked, which is worse than having no
// date at all: a wrong date is indistinguishable from a right one downstream.
// A non-positive value means the field was absent, and stays the zero time.
func leverCreatedAt(epochMillis int64) time.Time {
	if epochMillis <= 0 {
		return time.Time{}
	}

	return time.UnixMilli(epochMillis).UTC()
}

// leverDescription joins the two body blocks Lever publishes as plain text, for
// pay extraction only.
//
// Both are scanned because Lever splits a posting in half: descriptionPlain is
// the opening pitch and additionalPlain the closing block, and the
// pay-transparency paragraph is conventionally in the latter. The blank line
// between them keeps a sentence from one block running into the other, which
// matters because the extractor reads a fixed window of characters before a
// money figure to decide whether it is pay.
func leverDescription(description, additional string) string {
	description = strings.TrimSpace(description)
	additional = strings.TrimSpace(additional)

	switch {
	case description == "":
		return additional
	case additional == "":
		return description
	default:
		return description + "\n\n" + additional
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
