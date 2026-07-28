package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// pinpointPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const pinpointPlatform = "pinpoint"

func init() {
	registerBuiltin(pinpointPlatform, multiJobsFunc(Pinpoint, PinpointCompanies))
}

// PinpointCompanies holds the Pinpoint career sites this project crawls, one
// tenant subdomain per entry: "surrealdb" is https://surrealdb.pinpointhq.com.
//
// docs/source-backlog.md has tracked SurrealDB as a wanted company since the
// backlog was written, with a note that Pinpoint "is worth a shared pinpointhq
// service adapter" instead of a one-off scraper. This is that adapter, so the
// SurrealDB row and the platform note it carries are both closed by it.
//
// Pinpoint is the richest of the SMB lanes per request: one keyless GET returns
// a tenant's entire open-req list carrying department, employment type, a
// three-state workplace type — a distinction Remote *bool cannot express, which
// is why [internal.WorkplaceType] exists — and, for tenants that opt in, an
// employer-published pay range with its own period.
//
// # This list is measured, not staged
//
// Every entry below answered a live probe on 2026-07-28. The whole 119-slug
// candidate file at testdata/candidates/pinpoint_tenants.txt was probed at
// https://<slug>.pinpointhq.com/postings.json, one slug at a time, under the
// same shared-backend pacing internal/httpx applies to *.pinpointhq.com:
//
//   - 117 answered HTTP 200 with a non-empty "data" array whose elements carry
//     "title" and "url" at the top level, which is the promotion rule the
//     candidate file states. All 117 are registered here.
//   - 2 answered 200 with an empty "data" array: "chancetoshine" and
//     "savannainstitute". docs/adding-a-source.md is explicit that an empty
//     board is not a broken one, but it is also not evidence that a board is
//     live, so they stay in the candidate file unregistered.
//   - 0 were dead. Not one slug 404'd, and not one failed to resolve.
//
// The 117 together published 6,406 postings at the moment they were probed —
// about 55 postings per HTTP request, which makes this the cheapest lane per
// posting measured in this wave and roughly two and a half times the estimate
// docs/research/ats-platform-survey.md derived from the curator's annotations.
// Those annotations undercount badly at the top: "trilongroup" is annotated
// "~18" and answered with 957 postings.
//
// surrealdb is the one entry not drawn from the candidate file; its source is
// this repository's own docs/source-backlog.md. It is kept although it answered
// with an empty "data" array, because it is the tenant this platform was added
// for and an employer that is not hiring today is not a dead source.
//
// Slug ambiguity was the reason several entries were held back when this file
// was written blind, and the live probe settles those cases rather than
// guessing at them: "infor" serves 195 postings whose own URLs are on
// careers.infor.com, so it is the enterprise software vendor after all, and
// "aria", "article", "bright", "cfc", "field", "gig" and "magic" all answer
// with coherent single-employer req lists. No identity is asserted by
// registering them in any case: [internal.JobPosting.Company] is the tenant
// slug, and the URL published for every posting is the board's own.
var PinpointCompanies = []string{
	"aawdc",
	"agencyanalytics",
	"alcanzaclinical",
	"alcumus",
	"anthesisgroup",
	"appquantum",
	"aria",
	"armstrongwatson",
	"arrowglobal",
	"article",
	"avaaz",
	"bathspa",
	"bighatbiosciences",
	"bright",
	"british-business-bank",
	"btny",
	"c10labs",
	"cartesian",
	"carto",
	"cfc",
	"cfra",
	"chetwood-bank",
	"cnps",
	"coforma",
	"compasshealthnetwork",
	"confluence",
	"convexin",
	"cottonholdings",
	"crcna",
	"cubico",
	"davies",
	"dbrand",
	"deister",
	"digitalscience",
	"encompass",
	"eptura",
	"esss",
	"field",
	"franklin-electric",
	"fundapps",
	"gig",
	"goodenergy",
	"grasshopper",
	"groupgti",
	"groupo",
	"harnham",
	"hollandamericagroup",
	"icario",
	"impulsespace",
	"indrive",
	"infor",
	"inmusicbrands",
	"innovetivepetcare",
	"intandem",
	"intermedia",
	"invictus-verus",
	"jed",
	"judge-priestley",
	"keck",
	"kempinski",
	"kharon",
	"lbresearch",
	"londonyouth",
	"magic",
	"mcbains",
	"menzies",
	"mountainwarehouse",
	"multiplier-careers",
	"nasstar",
	"navigatepower",
	"nccgroup",
	"networkplus",
	"nmc",
	"nodalexchange",
	"northernbedrockcorps",
	"nypl",
	"oneplan",
	"oxfordmetrics",
	"penrosehealth",
	"pinnbank",
	"premierleague",
	"princesscruises",
	"pxlimited",
	"qac",
	"reconomy",
	"reimaginedcareers",
	"rockitmotors",
	"roofsbyaspen",
	"rwdi",
	"safetywing",
	"sandpiperci",
	"scandiweb",
	"shieldtp",
	"sjsustudentunion",
	"skims",
	"smartthings",
	"stacywitbeck",
	"sunking",
	"surgohealth",
	"surrealdb",
	"systematica",
	"tabby",
	"telesolvconsulting",
	"thearchco",
	"thorne",
	"togethergroup",
	"tradingtechnologies",
	"trilongroup",
	"twiningsovocareers",
	"uktv",
	"upway",
	"vgroup",
	"wearehuman8",
	"weoneil",
	"wolfe",
	"workwithus",
	"ymcaboston",
	"zenergi",
}

// pinpointPostingsResponse is one tenant's whole open-req list.
//
// Data is a pointer to a slice for the same reason recruiteeOffersResponse's is:
// an absent "data" key and an empty one are indistinguishable once decoded into
// a plain slice, and this project's worst failure is a source that quietly
// reports zero. A nil here means the envelope changed shape and is an error; a
// non-nil empty slice means the tenant is not hiring today, which
// docs/adding-a-source.md is explicit is not a failure.
//
// The envelope shape matters more here than on the other boards in this wave.
// "data" is also the container JSON:API uses, and under that convention each
// element's fields live in a nested "attributes" object rather than on the
// element itself. If Pinpoint's public feed is ever served that way, every
// posting below decodes into an empty struct — which is exactly the case the
// yielded-nothing check at the end of [Pinpoint] turns into a loud error rather
// than an empty board.
type pinpointPostingsResponse struct {
	Data *[]pinpointPosting `json:"data"`
}

// pinpointPosting is one opening on a Pinpoint career site.
//
// Only the fields this adapter publishes are modelled, per
// docs/adding-a-source.md. The same response also carries the description,
// key_responsibilities, skills_knowledge_expertise and benefits HTML blocks,
// which together are most of its bytes and which [internal.JobPosting] has
// nowhere to put.
type pinpointPosting struct {
	// ID is Pinpoint's own posting identifier, which outlives the URL.
	ID pinpointScalar `json:"id"`

	Title string `json:"title"`

	// URL is the public posting page. Pinpoint's own URL layout is not rebuilt
	// from parts when this is missing: it could not be verified from here, and a
	// guessed link that 404s is worse than a posting this adapter skips, because
	// a broken link looks like a real lead until it is clicked.
	URL string `json:"url"`

	// EmploymentTypeText is Pinpoint's human-facing spelling ("Full-time",
	// "Contract"), normalized rather than stored raw.
	EmploymentTypeText string `json:"employment_type_text"`

	// WorkplaceType is Pinpoint's three-state field. It is a real structured
	// answer, not a guess from location text, which is what makes it worth more
	// than [internal.JobPosting.IsRemote].
	//
	// The live spelling of the third state is "onsite", not the "on_site"
	// docs/research/ats-platform-survey.md documents: across the 6,406 postings
	// measured on 2026-07-28 the only three values were "onsite" (4,159),
	// "hybrid" (1,655) and "remote" (592), and "on_site" did not occur once.
	// Nothing needs to change for that, because [internal.NormalizeWorkplaceType]
	// squashes separators before comparing and both spellings reduce to the same
	// key — but the survey's value is wrong and an adapter that had matched it
	// literally would have mislabelled two thirds of the platform.
	WorkplaceType string `json:"workplace_type"`

	// CompensationVisible is the employer's own switch for showing pay. The
	// numbers are present in the response whether or not it is set, so reading
	// them without checking it would publish figures an employer deliberately
	// hid.
	CompensationVisible bool `json:"compensation_visible"`

	CompensationMinimum  pinpointScalar `json:"compensation_minimum"`
	CompensationMaximum  pinpointScalar `json:"compensation_maximum"`
	CompensationCurrency string         `json:"compensation_currency"`

	// CompensationFrequency is the interval the two bounds are quoted in.
	//
	// It is not in docs/research/ats-platform-survey.md, which lists only
	// minimum/maximum/currency/visible, and this adapter was originally written
	// asserting Pinpoint "publishes no period alongside them". A probe of all
	// 119 candidate tenants on 2026-07-28 found the key present on all 6,406
	// postings and populated on 3,002 of the 3,169 that show pay: "year" 1,676,
	// "hour" 1,202, "month" 113, "week" 7, "day" 2 and "two_weeks" 2.
	//
	// Reading it matters beyond tidiness. Without it [internal.Compensation]
	// infers the period from magnitude, and that heuristic only ever answers
	// hour or year — so every one of the 122 monthly, weekly and daily ranges
	// was being republished as an annual salary.
	CompensationFrequency string `json:"compensation_frequency"`

	Location struct {
		City     string `json:"city"`
		Province string `json:"province"`

		// Name is the location's own label, which is what carries a value like
		// "Remote" or a country for the postings with no city.
		Name string `json:"name"`
	} `json:"location"`

	Job struct {
		Department struct {
			Name string `json:"name"`
		} `json:"department"`
	} `json:"job"`
}

// pinpointScalar decodes a JSON value whose type Pinpoint does not hold stable
// into a string.
//
// The reference implementation this adapter was written against coerces both
// compensation bounds with float(), which accepts a JSON number and a numeric
// string alike, and stringifies the id without assuming its type. Modelling
// either as a Go float64 or int would let one tenant's odd value fail the decode
// of the whole response — fetchJSON decodes it in one call — and take every
// posting that company has with it. greenhouseScalar exists for the same reason
// on Greenhouse's requisition_id.
type pinpointScalar string

// UnmarshalJSON implements [json.Unmarshaler].
func (s *pinpointScalar) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))

	if trimmed == "" || trimmed == "null" {
		*s = ""

		return nil
	}

	if trimmed[0] == '"' {
		var text string

		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}

		*s = pinpointScalar(strings.TrimSpace(text))

		return nil
	}

	// An object or an array is neither an id nor an amount, and rendering its
	// literal JSON into the field would publish "{...}" as a salary.
	if trimmed[0] == '{' || trimmed[0] == '[' {
		*s = ""

		return nil
	}

	*s = pinpointScalar(trimmed)

	return nil
}

// amount reads the scalar as a pay figure, reporting false when it is not one.
func (s pinpointScalar) amount() (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64)
	if err != nil || value <= 0 {
		return 0, false
	}

	return value, true
}

// pinpointPeriods maps Pinpoint's compensation_frequency spelling onto
// [internal.Period]. Every key is a value measured in the live probe.
//
// "two_weeks" is deliberately absent. [internal.Period] has no fortnightly unit,
// and folding it into "week" would halve the figure a consumer reads while
// looking exactly like a correct answer. Leaving it unmapped sends those two
// postings back to the magnitude heuristic, which is the same place they were
// before this field was read at all.
var pinpointPeriods = map[string]internal.Period{
	"hour":    internal.PeriodHour,
	"hourly":  internal.PeriodHour,
	"day":     internal.PeriodDay,
	"daily":   internal.PeriodDay,
	"week":    internal.PeriodWeek,
	"weekly":  internal.PeriodWeek,
	"month":   internal.PeriodMonth,
	"monthly": internal.PeriodMonth,
	"year":    internal.PeriodYear,
	"yearly":  internal.PeriodYear,
	"annual":  internal.PeriodYear,
}

// pinpointPeriod parses Pinpoint's pay interval, returning
// [internal.PeriodUnknown] for anything unrecognised so that
// [internal.Compensation] falls back to inferring the period from magnitude
// rather than this adapter guessing one.
func pinpointPeriod(raw string) internal.Period {
	return pinpointPeriods[strings.ToLower(strings.TrimSpace(raw))]
}

// pinpointLocation renders the place a posting is offered at, preferring
// "City, Province" and falling back to the location's own label.
func pinpointLocation(posting pinpointPosting) string {
	var (
		city     = strings.TrimSpace(posting.Location.City)
		province = strings.TrimSpace(posting.Location.Province)
		name     = strings.TrimSpace(posting.Location.Name)
	)

	if city != "" && province != "" {
		return city + ", " + province
	}

	if city != "" {
		return city
	}

	if name != "" {
		return name
	}

	return "unknown"
}

// pinpointCompensation turns Pinpoint's pay fields into a range, returning nil
// when the employer published none or chose not to show it.
//
// Provenance is [internal.ProvenanceEmployer]: these are dedicated numeric
// fields, not figures read out of prose. The period comes from the board's own
// compensation_frequency where it published one, and falls back to
// [internal.Compensation]'s magnitude inference where it did not.
func pinpointCompensation(posting pinpointPosting) *internal.Compensation {
	if !posting.CompensationVisible {
		return nil
	}

	comp := &internal.Compensation{
		Currency:   strings.ToUpper(strings.TrimSpace(posting.CompensationCurrency)),
		Period:     pinpointPeriod(posting.CompensationFrequency),
		Provenance: internal.ProvenanceEmployer,
	}

	if minimum, ok := posting.CompensationMinimum.amount(); ok {
		comp.Min = minimum
	}

	if maximum, ok := posting.CompensationMaximum.amount(); ok {
		comp.Max = maximum
	}

	// A currency with no figures is not a pay range; publishing it would make
	// --has-pay match postings that disclose nothing.
	if comp.IsZero() {
		return nil
	}

	return comp
}

// Pinpoint returns all of the job postings for one Pinpoint career site, or an
// error if there was a problem making the request or parsing the response.
//
// company is the tenant's subdomain, see [PinpointCompanies].
//
// There is no pagination here, deliberately: /postings.json answers with the
// tenant's entire open-req list, so there is no page parameter for a board to
// ignore and no loop for [pageRepeatGuard] to bound. The equivalent hazard for a
// single-shot endpoint is a response that decodes cleanly into nothing, which is
// what the two shape checks below are for.
func Pinpoint(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.pinpointhq.com/
	// https://$company.pinpointhq.com/postings.json
	return func(yield func(*internal.JobPosting, error) bool) {
		postingsURL := "https://" + company + ".pinpointhq.com/postings.json"

		doc, err := fetchJSON[pinpointPostingsResponse](ctx, httpClient, "Pinpoint", company, jsonRequest{URL: postingsURL})
		if err != nil {
			yield(nil, err)

			return
		}

		if doc.Data == nil {
			yield(nil, fmt.Errorf("unexpected response shape from Pinpoint for company %q at %s: no %q key, so this is not the postings feed this adapter reads", company, postingsURL, "data"))

			return
		}

		postings := *doc.Data
		yielded := 0

		for _, posting := range postings {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			title := strings.TrimSpace(posting.Title)
			url := strings.TrimSpace(posting.URL)

			if title == "" || !strings.HasPrefix(url, "https://") {
				continue
			}

			jobPosting := &internal.JobPosting{
				Company:  company,
				URL:      url,
				Title:    title,
				Location: pinpointLocation(posting),

				Compensation: pinpointCompensation(posting),
				Department:   strings.TrimSpace(posting.Job.Department.Name),
				ExternalID:   strings.TrimSpace(string(posting.ID)),
				Source: internal.PostingSource{
					Platform: pinpointPlatform,
					Key:      company,
				},
			}

			// Pinpoint publishes no posted date at all, only an optional
			// application deadline, so PostedAt stays zero and these postings are
			// excluded from --posted-since queries. That is the honest outcome:
			// synthesising a date from the crawl time would make every posting
			// look new every night.
			//
			// Measured rather than assumed: the 6,406 postings captured on
			// 2026-07-28 carry exactly 25 keys between them, the same 25 on every
			// posting, and the only date among them is "deadline_at".

			if workplace, ok := internal.NormalizeWorkplaceType(posting.WorkplaceType); ok {
				jobPosting.WorkplaceType = workplace

				// Unlike every other board in this project, Pinpoint makes the
				// employer choose one of three values, so "not remote" here is a
				// statement rather than an absence: hybrid and onsite both mean
				// the role is not fully remote, and recording that is what stops
				// [internal.JobPosting.IsRemote] from re-deciding it by looking
				// for the word "remote" in a location string.
				remote := workplace == internal.WorkplaceTypeRemote

				jobPosting.Remote = &remote
			}

			// An unrecognised spelling leaves the field empty rather than
			// guessing: a wrong employment type cannot be told apart from a right
			// one by a filter, while an absent one is visibly absent.
			if employment, ok := internal.NormalizeEmploymentType(posting.EmploymentTypeText); ok {
				jobPosting.EmploymentType = employment
			}

			yielded++

			if !yield(jobPosting, nil) {
				return
			}
		}

		// A response full of postings that produced none at all means every one
		// of them was missing a title or an https URL, which no live board does.
		// It is the signature of a renamed field or of the JSON:API envelope
		// described on [pinpointPostingsResponse], and reporting zero postings
		// for it would be indistinguishable from a company that is not hiring.
		if len(postings) > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from Pinpoint for company %q at %s: %d postings decoded but none carried both a title and an https URL", company, postingsURL, len(postings)))
		}
	}
}
