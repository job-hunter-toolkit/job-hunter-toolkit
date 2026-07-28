package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// recruiteePlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const recruiteePlatform = "recruitee"

func init() {
	registerBuiltin(recruiteePlatform, multiJobsFunc(Recruitee, RecruiteeCompanies))
}

// RecruiteeCompanies holds the Recruitee career sites this project crawls, one
// tenant subdomain per entry: "bunq" is https://bunq.recruitee.com.
//
// docs/source-backlog.md has wanted this platform for a while, and it is one of
// the cheapest lanes in the project: one keyless GET returns a tenant's entire
// open-req list with department, employment type, remote flag, publish date and
// (for the tenants that opt in) an employer-published pay range already on it.
//
// # Why this list is short
//
// The research pass behind this adapter recovered 507 candidate slugs, and the
// container it ran in cannot reach a job board, so not one of them was probed
// here. Registering all 507 unverified would be reckless at this project's
// fan-out: every dead tenant burns a request per crawl, reports as a failing
// source, and enough of them together trip the Source Health workflow's
// 35%-failure alarm — the signal that is supposed to mean a real platform broke.
//
// So this is a staging subset and the full candidate list is committed verbatim,
// with its provenance headers, at testdata/candidates/recruitee_slugs.txt.
// Promoting the rest is mechanical work for a CI verification pass, the only
// place in this project with real network access. This adapter does not change
// when that happens.
//
// Selection rules for the entries below, in order:
//
//  1. Only slugs from the candidate file's hand-curated sections, whose headers
//     record a live probe of /api/offers/ with a non-zero offer count. Every
//     slug merged by the file's later automated apply-URL harvests is excluded
//     wholesale: those carry "?" where the employer name should be, so nobody
//     can say which company a slug even claims to be.
//  2. Slugs whose employer identity is unambiguous.
//     docs/adding-a-source.md documents that short generic slugs are
//     first-come-first-served and routinely belong to somebody other than the
//     famous holder of the name, so "make", "grid", "summit", "intent",
//     "parent" and "strata" are all left in the candidate file. "zara" is left
//     there too: a Recruitee tenant for Inditex's flagship brand is exactly the
//     shape of claim that needs a live identity check before it is believed.
//  3. Recruiting agencies and staffing firms are skipped where recognisable.
//     They republish one client's role many times, which inflates the crawl
//     without adding employers.
var RecruiteeCompanies = []string{
	"1x",
	"affideaitaly",
	"aihr",
	"bunq",
	"centreon",
	"channable",
	"dpgmedia",
	"equalsmoney",
	"exeon",
	"framestore",
	"greatminds",
	"greenchoice",
	"holded",
	"hostaway",
	"iliabeauty",
	"incentro",
	"intralot",
	"jobsdeerns",
	"kpsnacks",
	"legalfly",
	"lomography",
	"mgid",
	"moneyhash",
	"natuvion",
	"openclassrooms",
	"pacmed",
	"payconiq",
	"petalmd",
	"sequra",
	"snapeda",
	"timedoctor",
	"vandebron",
	"walibiholland",
	"wallarm",
	"werkenbijnlr",
}

// recruiteeOffersResponse is one tenant's whole open-req list.
//
// Offers is a pointer to a slice on purpose. An absent "offers" key and an empty
// one decode identically into a plain []recruiteeOffer, and a source that
// reports zero postings because the response stopped being the response this
// adapter parses is the silently-empty failure docs/architecture-roadmap.md
// treats as the worst one available. A nil here means the key was missing, which
// is a shape change and an error; a non-nil empty slice means the tenant is not
// hiring today, which docs/adding-a-source.md is explicit is not a failure.
type recruiteeOffersResponse struct {
	Offers *[]recruiteeOffer `json:"offers"`
}

// recruiteeOffer is one opening on a Recruitee career site.
//
// Only the fields this adapter actually publishes are modelled, per
// docs/adding-a-source.md: the same response also carries the full HTML
// description, requirements, tags and an applicant-form schema, none of which
// [internal.JobPosting] has anywhere to put.
type recruiteeOffer struct {
	// ID is Recruitee's own numeric posting id. It outlives the URL, which a
	// re-titled posting changes, and URL-keyed [internal.Dedupe] cannot follow
	// that.
	ID recruiteeScalar `json:"id"`

	// Slug is the posting's path segment, used to rebuild the public URL when
	// careers_url is absent.
	Slug  string `json:"slug"`
	Title string `json:"title"`

	// CareersURL is the public posting page on the tenant's career site.
	CareersURL string `json:"careers_url"`

	Department string `json:"department"`

	// EmploymentTypeCode is Recruitee's own vocabulary ("fulltime", "parttime",
	// "internship", "freelance", "temporary"), normalized rather than stored raw.
	EmploymentTypeCode string `json:"employment_type_code"`

	// ExperienceCode is Recruitee's seniority vocabulary, and its exact field
	// name could NOT be verified here: no request to recruitee.com is possible
	// from this container, and the reference implementation this adapter was
	// written against does not read it. It is decoded opportunistically — a
	// tenant that publishes it gets a [internal.JobPosting.Seniority] for free,
	// and if the field is named something else the value simply stays empty,
	// which is the same state as the many boards that publish no level at all.
	ExperienceCode string `json:"experience_code"`

	// Remote is Recruitee's structured remote flag.
	Remote *bool `json:"remote"`

	City     string `json:"city"`
	Country  string `json:"country"`
	Location string `json:"location"`

	// PublishedAt is when the posting went live, formatted like
	// "2026-05-28 20:36:05 UTC" rather than as ISO-8601.
	PublishedAt string `json:"published_at"`

	// UpdatedAt is decoded opportunistically and its name is likewise unverified
	// here. It is deliberately never used to stand in for PublishedAt: editing a
	// description does not make a nine-month-old req new, and
	// [internal.Filter.PostedSince] would then quietly fill a "posted this week"
	// query with stale postings.
	UpdatedAt string `json:"updated_at"`

	// Salary is the employer-published pay range, present only for the tenants
	// that opt into showing pay.
	Salary struct {
		Min      recruiteeScalar `json:"min"`
		Max      recruiteeScalar `json:"max"`
		Currency string          `json:"currency"`

		// Period is Recruitee's own interval spelling ("month", "year"). Its
		// field name is unverified here; when it is absent
		// [internal.Compensation] infers the period from the magnitude of the
		// figures, which is the documented fallback.
		Period string `json:"period"`
	} `json:"salary"`
}

// recruiteeScalar decodes a JSON value whose type Recruitee does not hold stable
// into a string.
//
// The reference implementation this adapter was written against reads the
// posting id straight into a string and guards each salary bound with an
// explicit numeric type check before using it, which is what a field that
// arrives sometimes as a number and sometimes as a string looks like from the
// outside. Modelling either as a Go float64 would make one such tenant fail to
// decode, and fetchJSON decodes the whole response at once, so a single odd
// salary would take down every posting that company has. That is the same
// failure greenhouseScalar exists to prevent on Greenhouse's requisition_id.
type recruiteeScalar string

// UnmarshalJSON implements [json.Unmarshaler].
func (s *recruiteeScalar) UnmarshalJSON(data []byte) error {
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

		*s = recruiteeScalar(strings.TrimSpace(text))

		return nil
	}

	// An object or an array is neither an id nor an amount, and rendering its
	// literal JSON into the field would publish "{...}" as a salary.
	if trimmed[0] == '{' || trimmed[0] == '[' {
		*s = ""

		return nil
	}

	*s = recruiteeScalar(trimmed)

	return nil
}

// amount reads the scalar as a pay figure, reporting false when it is not one.
func (s recruiteeScalar) amount() (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64)
	if err != nil || value <= 0 {
		return 0, false
	}

	return value, true
}

// recruiteePeriods maps Recruitee's interval spelling onto [internal.Period].
// Both the bare unit and the adverbial form are accepted because the field could
// not be probed and boards spell this either way.
var recruiteePeriods = map[string]internal.Period{
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

// recruiteePeriod parses Recruitee's salary interval, returning
// [internal.PeriodUnknown] for anything unrecognised so that
// [internal.Compensation] falls back to inferring the period from magnitude
// rather than this adapter guessing one.
func recruiteePeriod(raw string) internal.Period {
	return recruiteePeriods[strings.ToLower(strings.TrimSpace(raw))]
}

// recruiteeTimeLayouts are the shapes a Recruitee timestamp has been described
// as arriving in, most specific first.
//
// The documented one is "2026-05-28 20:36:05 UTC", which is neither RFC 3339 nor
// anything [time.Parse] recognises without being told. RFC 3339 is tried anyway
// because it costs nothing and a board that modernises its format should not
// silently lose its dates.
var recruiteeTimeLayouts = []string{
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05",
	time.RFC3339,
	"2006-01-02",
}

// recruiteeTime parses one of Recruitee's timestamps into UTC, returning the
// zero time when the board published none or the value cannot be read.
//
// An unreadable value yields the zero time rather than an error: one posting
// with an odd timestamp must not cost a board its other postings, and
// [internal.Filter.PostedSince] excludes undated postings anyway, so the failure
// mode is a posting missing from a date query rather than a wrong date in it.
func recruiteeTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}

	for _, layout := range recruiteeTimeLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			// Stored in UTC so comparing a Recruitee posting with an Ashby one is
			// a comparison of instants rather than of the zones two boards
			// happened to render in.
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// recruiteeLocation renders the place a posting is offered at.
//
// Recruitee publishes both a prebuilt "location" string and the city/country
// parts it was built from. The prebuilt one is preferred because it is what the
// employer sees on their own careers page; the parts are the fallback for the
// tenants that leave it empty.
func recruiteeLocation(offer recruiteeOffer) string {
	if location := strings.TrimSpace(offer.Location); location != "" {
		return location
	}

	parts := make([]string, 0, 2)

	for _, part := range []string{offer.City, offer.Country} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}

	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}

	if offer.Remote != nil && *offer.Remote {
		return "Remote"
	}

	return "unknown"
}

// recruiteeCompensation turns Recruitee's salary object into a pay range,
// returning nil when the tenant published none.
//
// Provenance is [internal.ProvenanceEmployer]: these are dedicated numeric
// fields the employer filled in, not figures read out of prose, and
// docs/compensation.md requires the two never be blended.
func recruiteeCompensation(offer recruiteeOffer) *internal.Compensation {
	comp := &internal.Compensation{
		Currency:   strings.ToUpper(strings.TrimSpace(offer.Salary.Currency)),
		Period:     recruiteePeriod(offer.Salary.Period),
		Provenance: internal.ProvenanceEmployer,
	}

	if minimum, ok := offer.Salary.Min.amount(); ok {
		comp.Min = minimum
	}

	if maximum, ok := offer.Salary.Max.amount(); ok {
		comp.Max = maximum
	}

	// A currency with no figures is not a pay range; publishing it would make
	// --has-pay match postings that disclose nothing.
	if comp.IsZero() {
		return nil
	}

	return comp
}

// recruiteeOfferURL returns the public posting page.
//
// careers_url is what the board publishes, and the "/o/<slug>" form is the same
// URL rebuilt from parts for the tenants that omit it. A posting with neither is
// dropped: this project's output is a list of links, and a posting without one
// is not a lead.
func recruiteeOfferURL(company string, offer recruiteeOffer) string {
	if url := strings.TrimSpace(offer.CareersURL); strings.HasPrefix(url, "https://") {
		return url
	}

	if slug := strings.TrimSpace(offer.Slug); slug != "" {
		return "https://" + company + ".recruitee.com/o/" + slug
	}

	return ""
}

// Recruitee returns all of the job postings for one Recruitee career site, or an
// error if there was a problem making the request or parsing the response.
//
// company is the tenant's subdomain, see [RecruiteeCompanies].
//
// There is no pagination here, deliberately: /api/offers/ answers with the
// tenant's entire open-req list in one response, so there is no page parameter
// for a board to ignore and no loop for [pageRepeatGuard] to bound. The
// equivalent hazard for a single-shot endpoint is a response that decodes
// cleanly into nothing, which is what the two shape checks below are for.
func Recruitee(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.recruitee.com/
	// https://$company.recruitee.com/api/offers/
	// https://$company.recruitee.com/o/$offer_slug
	return func(yield func(*internal.JobPosting, error) bool) {
		offersURL := "https://" + company + ".recruitee.com/api/offers/"

		doc, err := fetchJSON[recruiteeOffersResponse](ctx, httpClient, "Recruitee", company, jsonRequest{URL: offersURL})
		if err != nil {
			yield(nil, err)

			return
		}

		if doc.Offers == nil {
			yield(nil, fmt.Errorf("unexpected response shape from Recruitee for company %q at %s: no %q key, so this is not the offers feed this adapter reads", company, offersURL, "offers"))

			return
		}

		offers := *doc.Offers
		yielded := 0

		for _, offer := range offers {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			title := strings.TrimSpace(offer.Title)
			url := recruiteeOfferURL(company, offer)

			if title == "" || url == "" {
				continue
			}

			posting := &internal.JobPosting{
				Company:  company,
				URL:      url,
				Title:    title,
				Location: recruiteeLocation(offer),

				Compensation:  recruiteeCompensation(offer),
				Department:    strings.TrimSpace(offer.Department),
				WorkplaceType: recruiteeWorkplaceType(offer),
				Seniority:     strings.TrimSpace(offer.ExperienceCode),
				PostedAt:      recruiteeTime(offer.PublishedAt),
				UpdatedAt:     recruiteeTime(offer.UpdatedAt),
				ExternalID:    strings.TrimSpace(string(offer.ID)),
				Source: internal.PostingSource{
					Platform: recruiteePlatform,
					Key:      company,
				},
			}

			// Carried only when the board says true, exactly as workable.go
			// argues: remote=false says "not marked remote" and nothing more, and
			// storing it would switch off the location-text fallback in
			// [internal.JobPosting.IsRemote] for every posting on the platform.
			if offer.Remote != nil && *offer.Remote {
				remote := true

				posting.Remote = &remote
			}

			// An unrecognised spelling leaves the field empty rather than
			// guessing: a wrong employment type cannot be told apart from a right
			// one by a filter, while an absent one is visibly absent.
			if employment, ok := internal.NormalizeEmploymentType(offer.EmploymentTypeCode); ok {
				posting.EmploymentType = employment
			}

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// A response full of offers that produced no postings at all means every
		// one of them was missing a title or a URL, which no live board does. It
		// is the signature of a renamed field, and reporting zero postings for it
		// would be indistinguishable from a company that is not hiring.
		if len(offers) > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from Recruitee for company %q at %s: %d offers decoded but none carried both a title and a URL", company, offersURL, len(offers)))
		}
	}
}

// recruiteeWorkplaceType resolves where the work happens from the only
// structured field Recruitee publishes.
//
// The mapping is deliberately one-directional. remote=true is the board stating
// the role is remote, so it is evidence. remote=false only says the role is not
// fully remote, which leaves hybrid and onsite indistinguishable; mapping it to
// onsite would invent an office requirement the employer never stated, and
// [internal.WorkplaceTypeUnknown] documents that unknown is not onsite for
// exactly this reason.
func recruiteeWorkplaceType(offer recruiteeOffer) internal.WorkplaceType {
	if offer.Remote != nil && *offer.Remote {
		return internal.WorkplaceTypeRemote
	}

	return internal.WorkplaceTypeUnknown
}
