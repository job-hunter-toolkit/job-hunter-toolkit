package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// teamtailorPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const teamtailorPlatform = "teamtailor"

func init() {
	registerBuiltin(teamtailorPlatform, multiJobsFuncNamed(Teamtailor, TeamtailorCompanies, teamtailorCompanyName))
}

// teamtailorMaxPages bounds how many feed pages a single tenant may be asked
// for.
//
// The feed is a JSON Feed (jsonfeed.org), whose one pagination mechanism is a
// "next_url" the publisher puts in the document. Whether Teamtailor sets it
// could not be verified from this container; a tenant that does not simply ends
// after one request. What must not happen is the case the repo has just finished
// repairing across eight adapters: a feed whose next_url points back at a page
// already served loops until the crawl deadline, pinning a worker and hammering
// one host, while [internal.Dedupe] hides the duplicates so the posting total
// looks normal. [pageRepeatGuard] catches that; this is the backstop for a feed
// that keeps serving fresh pages forever. The largest tenant in the candidate
// list publishes roughly 200 openings, so 100 pages is far past any real board
// even if a page held a single posting.
const teamtailorMaxPages = 100

// TeamtailorCompanies holds the Teamtailor career sites this project crawls, one
// tenant subdomain per entry: "tibber" is https://tibber.teamtailor.com.
//
// The subdomain is used verbatim. It may carry a region label ("foothill.na") or
// an account-id suffix ("cameramatics-1639649453"); both are part of the host,
// so neither may be cleaned up before fetching. [teamtailorCompanyName] tidies
// the display name instead.
//
// Teamtailor answers one keyless GET with a tenant's entire open-req list as a
// JSON Feed, each item carrying a schema.org JobPosting block with employment
// type, locations and a telecommute flag. It is heavily Nordic and EU, which is
// coverage this project has very little of.
//
// # Why this list is short
//
// The research pass behind this adapter recovered 1,037 candidate slugs and
// could not probe one of them: nothing in this container can reach a job board.
// Registering them all unverified would be reckless at this project's fan-out —
// every dead tenant burns a request per crawl, reports as a failing source, and
// enough of them together trip the Source Health workflow's 35%-failure alarm,
// the signal that is supposed to mean a real platform broke. The full candidate
// list is committed verbatim with its provenance headers at
// testdata/candidates/teamtailor_slugs.txt, for a CI verification pass — the
// only place in this project with real network access — to promote from. This
// adapter does not change when that happens.
//
// Selection rules for the entries below, in order:
//
//  1. Only slugs from the candidate file's hand-curated sections, whose headers
//     record a live probe of /jobs.json with a non-zero item count and name the
//     employer behind each slug. Everything merged by the file's later automated
//     apply-URL harvests is excluded wholesale.
//  2. Highest annotated open-req counts first, because postings per HTTP request
//     is the metric that matters for a crawl that already misses its deadline.
//  3. Employers whose identity the slug makes unambiguous, per
//     docs/adding-a-source.md's warning about short generic slugs. "sweep",
//     "power", "combine", "centio" and "kosmos" stay in the candidate file for
//     that reason — and "sweep" doubly so, since this project already registers
//     a different company under that name on Ashby.
//  4. Recruiting agencies and RPOs are skipped where recognisable; they
//     republish one client's role many times, inflating the crawl without adding
//     employers.
//
// cameramatics-1639649453 is kept deliberately even though it is small: it is
// the account-id-suffixed shape, and having one in the crawl keeps
// [teamtailorCompanyName] honest against a real tenant rather than only against
// a fixture.
var TeamtailorCompanies = []string{
	"andritzdigitalfactory",
	"anyfin",
	"bearingpoint",
	"cameramatics-1639649453",
	"chalhoubgroup",
	"cigames",
	"ecoonline",
	"footasylum",
	"funnel",
	"huaweiresearchcentergermanyaustria",
	"idealista",
	"imedhospitales",
	"ingeteam",
	"instabee",
	"iqm",
	"leaseweb",
	"luminorbank",
	"mintos",
	"nextlane",
	"normative",
	"oneflow",
	"paysend",
	"remarkable",
	"seedtag",
	"smartestenergy",
	"stillfrontgroup",
	"superawesome",
	"teamlewis",
	"tibber",
	"tradingview",
	"ucademy",
	"virtasant",
	"wspcentraleurope",
	"wspfrance",
}

// teamtailorAccountSuffix matches the account-id suffix some Teamtailor
// subdomains carry, such as the "-1639649453" of "cameramatics-1639649453".
//
// The number is a unix timestamp from the account's creation, so it is at least
// nine digits and cannot be confused with a company name that merely ends in a
// number ("chapter-2", "360t"). Anchored and length-bounded for exactly that
// reason.
var teamtailorAccountSuffix = regexp.MustCompile(`-\d{9,}$`)

// teamtailorCompanyName derives the display name for a Teamtailor tenant from
// its subdomain.
//
// The subdomain is the fetch key and must stay verbatim; this is only what a
// person sees in `job-hunter-toolkit companies` and in a posting's company
// field. [SourcesMatching] matches both forms, so `--company
// cameramatics-1639649453` and `--company cameramatics` still select the same
// source.
func teamtailorCompanyName(slug string) string {
	trimmed := strings.TrimSpace(slug)

	name := teamtailorAccountSuffix.ReplaceAllString(trimmed, "")
	if name == "" {
		return trimmed
	}

	return name
}

// teamtailorFeed is one page of a tenant's JSON Feed.
//
// Items is a pointer to a slice on purpose. An absent "items" key and an empty
// one decode identically into a plain slice, and a source that reports zero
// postings because the response stopped being the response this adapter parses
// is the silently-empty failure docs/architecture-roadmap.md treats as the worst
// one available. A nil here means this is not a JSON Feed at all — a redirect to
// a marketing page, an error document — and is an error; a non-nil empty slice
// means the tenant is not hiring today, which docs/adding-a-source.md is
// explicit is not a failure.
type teamtailorFeed struct {
	Items *[]teamtailorItem `json:"items"`

	// NextURL is JSON Feed's pagination link. See [teamtailorMaxPages] for why
	// it is followed at all and [teamtailorNextPageURL] for what is required of
	// it before it is.
	NextURL string `json:"next_url"`
}

// teamtailorItem is one opening in the feed.
//
// Only the fields this adapter publishes are modelled, per
// docs/adding-a-source.md. content_html — the entire posting body, and most of
// the feed's bytes — is deliberately not decoded: [internal.JobPosting] has
// nowhere to put it.
type teamtailorItem struct {
	// ID is the feed item's own identifier, a UUID. It is the last resort for
	// [internal.JobPosting.ExternalID]; the numeric posting id that the public
	// URL is built from is preferred, since that is the one a person or a
	// Teamtailor API user would recognise.
	ID string `json:"id"`

	URL   string `json:"url"`
	Title string `json:"title"`

	DatePublished string `json:"date_published"`
	DateModified  string `json:"date_modified"`

	// JobPosting is the schema.org JobPosting Teamtailor embeds in each item,
	// which is where every structured field on this platform lives.
	JobPosting teamtailorJobPosting `json:"_jobposting"`
}

// teamtailorJobPosting is the schema.org block embedded in a feed item.
type teamtailorJobPosting struct {
	// Identifier is schema.org's PropertyValue holding the numeric posting id,
	// the same one that leads the /jobs/{id}-{slug} URL.
	Identifier teamtailorText `json:"identifier"`

	// EmploymentType is schema.org's vocabulary ("FULL_TIME", "PART_TIME",
	// "INTERN", "CONTRACTOR"), normalized rather than stored raw.
	EmploymentType teamtailorText `json:"employmentType"`

	// JobLocationType is "TELECOMMUTE" for remote roles and absent otherwise;
	// schema.org defines no hybrid or onsite value, so its absence says nothing.
	JobLocationType teamtailorText `json:"jobLocationType"`

	// OccupationalCategory is schema.org's field for the job family, and is
	// decoded opportunistically: whether Teamtailor populates it from a board's
	// departments could NOT be verified from this container. A tenant that sets
	// it gets a [internal.JobPosting.Department] for free; the rest leave the
	// field empty, which is the same state as a board that publishes no
	// department at all.
	OccupationalCategory teamtailorText `json:"occupationalCategory"`

	// DatePosted is schema.org's publication date, used only when the feed item
	// carries no date_published of its own.
	DatePosted string `json:"datePosted"`

	JobLocation teamtailorPlaces `json:"jobLocation"`

	// BaseSalary is schema.org's MonetaryAmount, filled from the pay range an
	// employer typed into Teamtailor's own salary field. Measured on 2026-07-28
	// across 16,759 live postings from 1,033 tenants: present on 2,315 of them,
	// which makes it the only enrichment field on this platform that a real board
	// actually populates.
	BaseSalary *teamtailorSalary `json:"baseSalary"`
}

// teamtailorSalary is the schema.org MonetaryAmount a posting's pay is published
// as.
//
// Shape measured on 2026-07-28 across 69 paying postings from 61 tenants, which
// agreed on it exactly: currency is a bare ISO 4217 string that may be null or
// empty, and value is always a nested QuantitativeValue rather than a scalar.
type teamtailorSalary struct {
	Currency teamtailorText `json:"currency"`

	Value teamtailorQuantity `json:"value"`
}

// teamtailorQuantity is the schema.org QuantitativeValue inside a MonetaryAmount.
//
// A posting publishes either a range (minValue/maxValue) or a single figure
// (value); both spellings were measured, and both arrive as JSON strings rather
// than numbers. [teamtailorText] is what makes the number spelling work too,
// since schema.org permits it and a tenant is free to switch.
type teamtailorQuantity struct {
	UnitText teamtailorText `json:"unitText"`

	Value    teamtailorText `json:"value"`
	MinValue teamtailorText `json:"minValue"`
	MaxValue teamtailorText `json:"maxValue"`
}

// teamtailorPeriods maps schema.org's unitText vocabulary onto [internal.Period].
//
// HOUR, MONTH and YEAR are the three spellings measured live. DAY and WEEK are
// the rest of schema.org's vocabulary for this property and are accepted so a
// tenant that uses one is read rather than silently losing its period — the cost
// of being wrong is nil, since an unrecognised value already falls back to
// [internal.Compensation]'s magnitude inference.
var teamtailorPeriods = map[string]internal.Period{
	"HOUR":  internal.PeriodHour,
	"DAY":   internal.PeriodDay,
	"WEEK":  internal.PeriodWeek,
	"MONTH": internal.PeriodMonth,
	"YEAR":  internal.PeriodYear,
}

// teamtailorAmount reads one of schema.org's pay figures, reporting false when
// the field is absent or is not a positive number.
func teamtailorAmount(value teamtailorText) (float64, bool) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(value.String()), 64)
	if err != nil || amount <= 0 {
		return 0, false
	}

	return amount, true
}

// teamtailorCurrency returns the ISO 4217 code the feed published, or "" when it
// published none or published something that is not one.
//
// Measured: 8 of 69 paying postings carried a null currency and 2 an empty
// string, so an absent code is normal rather than exceptional.
// [internal.Compensation] documents Currency as often empty for exactly this
// reason, and a figure with no currency is still worth more than no figure.
func teamtailorCurrency(value teamtailorText) string {
	code := strings.ToUpper(strings.TrimSpace(value.String()))
	if len(code) != 3 {
		return ""
	}

	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}

	return code
}

// teamtailorCompensation turns a posting's baseSalary into a pay range,
// returning nil when the employer published none.
//
// Provenance is [internal.ProvenanceEmployer]: this is a dedicated field an
// employer filled in on the board, not a figure recovered from prose, and
// docs/compensation.md requires the two never be blended. The description is
// deliberately not searched as a fallback — [teamtailorItem] does not decode
// content_html at all, and holding 16,759 full job descriptions in memory to
// guess at pay would cost the crawl far more than the field is worth.
func teamtailorCompensation(salary *teamtailorSalary) *internal.Compensation {
	if salary == nil {
		return nil
	}

	comp := &internal.Compensation{
		Currency:   teamtailorCurrency(salary.Currency),
		Period:     teamtailorPeriods[strings.ToUpper(strings.TrimSpace(salary.Value.UnitText.String()))],
		Provenance: internal.ProvenanceEmployer,
	}

	if minimum, ok := teamtailorAmount(salary.Value.MinValue); ok {
		comp.Min = minimum
	}

	if maximum, ok := teamtailorAmount(salary.Value.MaxValue); ok {
		comp.Max = maximum
	}

	// A posting that names one figure rather than a range publishes it under
	// "value". It is a point, not a bound, so it fills both ends: reporting it as
	// a minimum alone would make --max-pay queries miss it entirely.
	if comp.Min == 0 && comp.Max == 0 {
		if amount, ok := teamtailorAmount(salary.Value.Value); ok {
			comp.Min, comp.Max = amount, amount
		}
	}

	// A currency or a period with no figures behind it is not a pay range, and
	// publishing one would make --has-pay match postings that disclose nothing.
	if comp.IsZero() {
		return nil
	}

	return comp
}

// teamtailorPlace is one schema.org Place a posting is offered at.
type teamtailorPlace struct {
	Address struct {
		AddressLocality teamtailorText `json:"addressLocality"`
		AddressRegion   teamtailorText `json:"addressRegion"`
		AddressCountry  teamtailorText `json:"addressCountry"`
	} `json:"address"`
}

// teamtailorPlaces is a jobLocation that may arrive as one Place or as a list of
// them.
//
// schema.org properties are single-or-repeated by definition, and a JSON-LD
// producer is free to emit either form for the same field on two postings.
// Modelling it as a plain slice would make a tenant that publishes one location
// per posting fail to decode, and fetchJSON decodes a whole page at once, so
// that one field would take down every posting on the page.
type teamtailorPlaces []teamtailorPlace

// UnmarshalJSON implements [json.Unmarshaler]. It never reports an error: a
// location shape this cannot read leaves the list empty, which is the same state
// as a posting with no location — something the rest of the pipeline already
// handles — whereas an error would delete every posting on the page.
func (p *teamtailorPlaces) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))

	if trimmed == "" || trimmed == "null" {
		return nil
	}

	if trimmed[0] == '[' {
		var places []teamtailorPlace

		if err := json.Unmarshal(data, &places); err == nil {
			*p = places
		}

		return nil
	}

	var place teamtailorPlace

	if err := json.Unmarshal(data, &place); err == nil {
		*p = teamtailorPlaces{place}
	}

	return nil
}

// teamtailorText is a schema.org value that may be written as a string, as a
// number, as a node object carrying the value under "value" or "name", or as a
// list of any of those.
//
// This is not defensive programming for its own sake: schema.org's whole model
// is that a property's object can be a literal or a node, so "addressCountry"
// is "SE" on one board and {"@type":"Country","name":"SE"} on the next, and
// "identifier" is 12345 here and a PropertyValue there. Every one of those is
// valid, and a Go string field would fail the decode of the entire page on the
// first one that is not a bare string.
type teamtailorText string

// UnmarshalJSON implements [json.Unmarshaler]. Like [smartRecruitersLabel] it
// never reports an error, for the same reason: an enrichment field must not be
// able to delete a tenant's postings.
func (t *teamtailorText) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))

	if trimmed == "" || trimmed == "null" {
		return nil
	}

	switch trimmed[0] {
	case '"':
		var text string

		if err := json.Unmarshal(data, &text); err == nil {
			*t = teamtailorText(strings.TrimSpace(text))
		}
	case '{':
		var node struct {
			Value teamtailorText `json:"value"`
			Name  teamtailorText `json:"name"`
		}

		if err := json.Unmarshal(data, &node); err == nil {
			// "value" first: a schema.org PropertyValue carries the identifier
			// under it and a human label under "name", and the identifier is the
			// half worth keeping.
			if node.Value != "" {
				*t = node.Value
			} else {
				*t = node.Name
			}
		}
	case '[':
		var list []teamtailorText

		if err := json.Unmarshal(data, &list); err == nil {
			for _, entry := range list {
				if entry != "" {
					*t = entry

					break
				}
			}
		}
	default:
		// A bare number, which is how the numeric posting id usually arrives.
		*t = teamtailorText(trimmed)
	}

	return nil
}

// String returns the text.
func (t teamtailorText) String() string { return string(t) }

// teamtailorTimeLayouts are the shapes a Teamtailor timestamp arrives in.
//
// JSON Feed requires RFC 3339 for date_published and date_modified, and the
// embedded schema.org block frequently carries a bare date instead.
var teamtailorTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02",
}

// teamtailorTime parses one of Teamtailor's timestamps into UTC, returning the
// zero time when the feed published none or the value cannot be read.
//
// An unreadable value yields the zero time rather than an error: one posting
// with an odd timestamp must not cost a board its other postings, and
// [internal.Filter.PostedSince] excludes undated postings anyway, so the failure
// mode is a posting missing from a date query rather than a wrong date in it.
func teamtailorTime(values ...string) time.Time {
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}

		for _, layout := range teamtailorTimeLayouts {
			if parsed, err := time.Parse(layout, text); err == nil {
				// Stored in UTC so comparing a Teamtailor posting with an Ashby
				// one is a comparison of instants rather than of the zones two
				// boards happened to render in.
				return parsed.UTC()
			}
		}
	}

	return time.Time{}
}

// teamtailorLocation renders the places a posting is offered at, one entry per
// schema.org Place, in the feed's own order.
func teamtailorLocation(places teamtailorPlaces, remote bool) string {
	names := make([]string, 0, len(places)+1)

	for _, place := range places {
		parts := []string{
			place.Address.AddressLocality.String(),
			place.Address.AddressRegion.String(),
			place.Address.AddressCountry.String(),
		}

		parts = slices.DeleteFunc(parts, func(part string) bool {
			return strings.TrimSpace(part) == ""
		})

		if name := strings.Join(parts, ", "); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	if remote && !slices.Contains(names, "Remote") {
		names = append(names, "Remote")
	}

	if len(names) == 0 {
		return "unknown"
	}

	return strings.Join(names, "; ")
}

// teamtailorPostingID returns the identifier this project stores for a feed
// item.
//
// The numeric schema.org identifier is preferred, then the leading token of the
// public URL's "/jobs/{id}-{slug}" path, which is that same number, and only
// then the feed item's UUID. All three are stable across a re-title; the first
// two are what a person reading the URL, or a Teamtailor API user, would
// recognise as the posting's id.
func teamtailorPostingID(item teamtailorItem) string {
	if identifier := strings.TrimSpace(item.JobPosting.Identifier.String()); identifier != "" {
		return identifier
	}

	if _, tail, found := strings.Cut(item.URL, "/jobs/"); found {
		if id, _, _ := strings.Cut(tail, "-"); id != "" {
			return id
		}
	}

	return strings.TrimSpace(item.ID)
}

// teamtailorNextPageURL resolves a feed's next_url against the page it came
// from, reporting an error for a link this crawl will not follow.
//
// A next_url is a URL a third party chose, and this adapter would otherwise
// fetch it with the project's client. It is required to stay on the tenant's own
// host or elsewhere under teamtailor.com, so a compromised or merely creative
// feed cannot redirect the crawl at an unrelated service under this project's
// User-Agent, and cannot move a board's traffic outside the limiter key the
// teamtailor.com hosts share. A link that fails those checks is reported rather
// than ignored: silently stopping there would truncate the board, and a source
// that returns half a company's jobs while looking healthy is the failure mode
// this project cares most about.
func teamtailorNextPageURL(currentURL, next string) (string, error) {
	next = strings.TrimSpace(next)
	if next == "" {
		return "", nil
	}

	base, err := url.Parse(currentURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse current feed URL %q: %w", currentURL, err)
	}

	parsed, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("failed to parse next_url %q: %w", next, err)
	}

	resolved := base.ResolveReference(parsed)

	if resolved.Scheme != "https" {
		return "", fmt.Errorf("refusing to follow next_url %q: not https", resolved)
	}

	host := strings.ToLower(resolved.Hostname())
	if host != strings.ToLower(base.Hostname()) && !strings.HasSuffix(host, ".teamtailor.com") {
		return "", fmt.Errorf("refusing to follow next_url %q: it leaves %s", resolved, base.Hostname())
	}

	// A feed that points at itself is the end of the feed, not a page to fetch
	// again. [pageRepeatGuard] would stop the loop one request later anyway; this
	// spends nothing to find that out.
	if resolved.String() == currentURL {
		return "", nil
	}

	return resolved.String(), nil
}

// teamtailorPage fetches one page of a tenant's feed.
//
// It is a function rather than an inline fetch so the response body is closed as
// each page is read, per docs/adding-a-source.md: deferring inside the loop would
// hold every page's body open for the rest of the crawl.
func teamtailorPage(ctx context.Context, httpClient *http.Client, company, feedURL string) (*teamtailorFeed, error) {
	feed, err := fetchJSON[teamtailorFeed](ctx, httpClient, "Teamtailor", company, jsonRequest{URL: feedURL})
	if err != nil {
		return nil, err
	}

	if feed.Items == nil {
		return nil, fmt.Errorf("unexpected response shape from Teamtailor for company %q at %s: no %q key, so this is not the JSON Feed this adapter reads", company, feedURL, "items")
	}

	return feed, nil
}

// Teamtailor returns all of the job postings for one Teamtailor career site, or
// an error if there was a problem making the request or parsing the response.
//
// company is the tenant's subdomain, see [TeamtailorCompanies].
func Teamtailor(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.teamtailor.com/jobs
	// https://$company.teamtailor.com/jobs.json
	// https://$company.teamtailor.com/jobs/$id-$slug
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			companyName = teamtailorCompanyName(company)
			feedURL     = "https://" + company + ".teamtailor.com/jobs.json"
			pages       pageRepeatGuard
			items       int
			yielded     int
		)

		for range teamtailorMaxPages {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			feed, err := teamtailorPage(ctx, httpClient, company, feedURL)
			if err != nil {
				yield(nil, err)

				return
			}

			ids := make([]string, 0, len(*feed.Items))
			for _, item := range *feed.Items {
				ids = append(ids, teamtailorPostingID(item))
			}

			// A feed that answers the next page with the page just served is not
			// paginating, and following its next_url again would repeat this
			// page until the crawl deadline.
			if pages.repeated(ids) {
				return
			}

			for _, item := range *feed.Items {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				items++

				title := strings.TrimSpace(item.Title)
				postingURL := strings.TrimSpace(item.URL)

				if title == "" || !strings.HasPrefix(postingURL, "https://") {
					continue
				}

				isRemote := teamtailorIsRemote(item)

				posting := &internal.JobPosting{
					Company:  companyName,
					URL:      postingURL,
					Title:    title,
					Location: teamtailorLocation(item.JobPosting.JobLocation, isRemote),

					Compensation: teamtailorCompensation(item.JobPosting.BaseSalary),

					Department: strings.TrimSpace(item.JobPosting.OccupationalCategory.String()),
					PostedAt:   teamtailorTime(item.DatePublished, item.JobPosting.DatePosted),
					UpdatedAt:  teamtailorTime(item.DateModified),
					ExternalID: teamtailorPostingID(item),
					Source: internal.PostingSource{
						Platform: teamtailorPlatform,
						Key:      company,
					},
				}

				// Carried only when the feed says TELECOMMUTE. schema.org has no
				// value meaning "office required", so its absence says nothing at
				// all, and storing false would switch off the location-text
				// fallback in [internal.JobPosting.IsRemote] for the whole
				// platform — the mistake workable.go documents.
				if isRemote {
					remote := true

					posting.Remote = &remote
					posting.WorkplaceType = internal.WorkplaceTypeRemote
				}

				// An unrecognised spelling leaves the field empty rather than
				// guessing: a wrong employment type cannot be told apart from a
				// right one by a filter, while an absent one is visibly absent.
				if employment, ok := internal.NormalizeEmploymentType(item.JobPosting.EmploymentType.String()); ok {
					posting.EmploymentType = employment
				}

				yielded++

				if !yield(posting, nil) {
					return
				}
			}

			next, err := teamtailorNextPageURL(feedURL, feed.NextURL)
			if err != nil {
				yield(nil, fmt.Errorf("cannot continue paginating Teamtailor for company %q at %s: %w", company, feedURL, err))

				return
			}

			if next == "" {
				// A feed with items but no postings at all means every item was
				// missing a title or an https URL, which no live board does. It is
				// the signature of a renamed field, and reporting zero postings
				// for it would be indistinguishable from a company that is not
				// hiring.
				if items > 0 && yielded == 0 {
					yield(nil, fmt.Errorf("unexpected response shape from Teamtailor for company %q at %s: %d feed items decoded but none carried both a title and an https URL", company, feedURL, items))
				}

				return
			}

			feedURL = next
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Teamtailor for company %q: the feed was still linking to another page after %d pages", company, teamtailorMaxPages))
	}
}

// teamtailorIsRemote reports whether the feed marked this posting as remote.
func teamtailorIsRemote(item teamtailorItem) bool {
	workplace, ok := internal.NormalizeWorkplaceType(item.JobPosting.JobLocationType.String())

	return ok && workplace == internal.WorkplaceTypeRemote
}
