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
// employer-published pay range.
//
// # Why this list is short
//
// The research pass behind this adapter recovered 119 candidate slugs and could
// not probe one of them: nothing in this container can reach a job board. At
// this project's fan-out an unverified tenant is not free — it burns a request
// every crawl, reports as a failing source, and enough of them together trip the
// Source Health workflow's 35%-failure alarm, which is the signal that is
// supposed to mean a real platform broke. The full candidate list is committed
// verbatim with its provenance headers at
// testdata/candidates/pinpoint_tenants.txt, for a CI verification pass to
// promote from; this adapter does not change when that happens.
//
// Selection rules for the entries below, in order:
//
//  1. Only slugs from the candidate file's two hand-curated batches, whose
//     header records a live probe returning HTTP 200 with a non-empty "data"
//     array and names the employer behind each slug. The file's later automated
//     apply-URL harvest is excluded wholesale: its rows carry "?" instead of an
//     employer name, so there is nothing to check an identity against.
//  2. Highest annotated open-req counts first, because postings per HTTP request
//     is the metric that matters for a crawl that already misses its deadline.
//  3. Employers whose identity the slug makes unambiguous, per
//     docs/adding-a-source.md's warning that short generic slugs usually belong
//     to somebody other than the famous holder of the name. "cfc", "aria",
//     "field", "magic", "article", "bright" and "gig" stay in the candidate file
//     for that reason, and so does "infor": a Pinpoint tenant for an enterprise
//     software vendor that size is a claim worth checking live first.
//  4. Recruiting and staffing agencies are skipped where recognisable; they
//     republish one client's role many times.
//
// surrealdb is the one entry not drawn from the candidate file. Its source is
// this repository's own docs/source-backlog.md, which recorded the tenant URL
// from a live fingerprinting pass.
var PinpointCompanies = []string{
	"arrowglobal",
	"bathspa",
	"british-business-bank",
	"carto",
	"coforma",
	"compasshealthnetwork",
	"confluence",
	"davies",
	"digitalscience",
	"franklin-electric",
	"fundapps",
	"goodenergy",
	"hollandamericagroup",
	"impulsespace",
	"indrive",
	"inmusicbrands",
	"kempinski",
	"nccgroup",
	"networkplus",
	"nypl",
	"penrosehealth",
	"premierleague",
	"princesscruises",
	"reconomy",
	"reimaginedcareers",
	"safetywing",
	"skims",
	"sunking",
	"surrealdb",
	"trilongroup",
	"uktv",
	"upway",
	"vgroup",
	"ymcaboston",
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

	// WorkplaceType is Pinpoint's three-state field: "remote", "hybrid" or
	// "on_site". It is a real structured answer, not a guess from location text,
	// which is what makes it worth more than [internal.JobPosting.IsRemote].
	WorkplaceType string `json:"workplace_type"`

	// CompensationVisible is the employer's own switch for showing pay. The
	// numbers are present in the response whether or not it is set, so reading
	// them without checking it would publish figures an employer deliberately
	// hid.
	CompensationVisible bool `json:"compensation_visible"`

	CompensationMinimum  pinpointScalar `json:"compensation_minimum"`
	CompensationMaximum  pinpointScalar `json:"compensation_maximum"`
	CompensationCurrency string         `json:"compensation_currency"`

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
// fields, not figures read out of prose. No period is published alongside them,
// so [internal.Compensation] infers one from the magnitude of the figures —
// which is how a frontline hourly rate and a salaried range stay comparable.
func pinpointCompensation(posting pinpointPosting) *internal.Compensation {
	if !posting.CompensationVisible {
		return nil
	}

	comp := &internal.Compensation{
		Currency:   strings.ToUpper(strings.TrimSpace(posting.CompensationCurrency)),
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
