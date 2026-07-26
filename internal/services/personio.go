package services

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// personioPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const personioPlatform = "personio"

func init() {
	registerBuiltin(personioPlatform, multiJobsFuncNamed(Personio, PersonioCompanies, personioCompanyName))
}

// personioMaxFeedBytes bounds one tenant's feed.
//
// The XML carries every posting's full multi-section HTML description, so it is
// the largest per-tenant response in this wave by an order of magnitude, and a
// crawl of hundreds of tenants runs them concurrently. The limit is a guard
// against one pathological tenant, not a target: the largest annotated tenant in
// the candidate list publishes about 190 openings, which is far below this even
// with generous descriptions.
const personioMaxFeedBytes = 32 << 20

// PersonioCompanies holds the Personio career sites this project crawls.
//
// An entry is normally the bare subdomain label — "holidu" is
// https://holidu.jobs.personio.de — but it may also be a full host for the
// tenants that publish on .com instead of .de, which is a real minority on this
// platform. Anything containing a dot is treated as a host verbatim, and nothing
// else can be: a Personio subdomain is a single DNS label, so a dot in a key
// cannot be part of one. That is what keeps a .com tenant from needing an
// adapter change, see [personioHost].
//
// Personio is the dominant DACH/EU SMB HR suite, and its career sites publish a
// keyless XML feed carrying department, employment type, schedule, seniority and
// a creation date for every open req in a single GET. This project's coverage of
// the German-speaking mid-market is otherwise close to nil.
//
// # Why this list is short
//
// The research pass behind this adapter recovered 999 candidate slugs and could
// not probe one of them: nothing in this container can reach a job board. At this
// project's fan-out an unverified tenant is not free — it burns a request every
// crawl, reports as a failing source, and enough of them together trip the Source
// Health workflow's 35%-failure alarm, the signal that is supposed to mean a real
// platform broke. The full candidate list is committed verbatim with its
// provenance headers at testdata/candidates/personio_slugs.txt, for a CI
// verification pass — the only place in this project with real network access —
// to promote from. This adapter does not change when that happens.
//
// Selection rules for the entries below, in order:
//
//  1. Only slugs from the candidate file's hand-curated sections, whose headers
//     record a live probe of /xml with a non-zero position count and name the
//     employer behind each slug. Everything merged by the file's later automated
//     apply-URL harvests is excluded wholesale.
//  2. Highest annotated open-req counts first, because postings per HTTP request
//     is the metric that matters for a crawl that already misses its deadline.
//  3. Employers whose identity the slug makes unambiguous, per
//     docs/adding-a-source.md's warning about short generic slugs; "stark",
//     "penta", "anton", "neos" and "gnosis" stay in the candidate file for that
//     reason.
//  4. Publishing the feed at all is a per-tenant switch on this platform (the
//     operator enables it under the career-page settings), so a tenant that has
//     not is a 404 rather than an empty feed. That is one more reason not to bulk
//     register the tail.
//
// "personio" is Personio's own board. It is small and it is kept on purpose: it
// is the tenant most likely to still exist and to still speak this format after
// a vendor change, which makes it the canary for a feed-shape break.
var PersonioCompanies = []string{
	"1komma5grad",
	"360t",
	"alasco",
	"apheris",
	"armedangels",
	"cabify",
	"chrono24",
	"circula",
	"clark",
	"consileon",
	"deepset",
	"dfb",
	"docuware",
	"egym",
	"everphone",
	"fieldfisher",
	"flatpay",
	"hirschen-group",
	"holidu",
	"homeserve",
	"init-ag",
	"instagrid",
	"joblinge",
	"knime",
	"lush",
	"macaw",
	"meteocontrol",
	"miles-mobility",
	"ohpen",
	"ororatech",
	"ottonova",
	"personio",
	"sevdesk",
	"wandelbots",
	"wilken",
	"wunderflats",
	"zollsoft",
}

// personioHost returns the career-site host for a tenant key.
//
// A key with no dot is a subdomain label on the platform's default domain, which
// is what almost every tenant is. A key with a dot is a full host, which is how a
// .com tenant is registered without this adapter learning a per-tenant domain
// table. A key written as a URL is accepted too, because somebody will
// eventually paste one.
func personioHost(key string) string {
	host := strings.TrimSpace(key)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host, _, _ = strings.Cut(host, "/")

	if strings.Contains(host, ".") {
		return host
	}

	return host + ".jobs.personio.de"
}

// personioCompanyName derives the display name for a tenant from its key, which
// is the leading label either way.
func personioCompanyName(key string) string {
	host := personioHost(key)

	label, _, _ := strings.Cut(host, ".")

	return label
}

// personioFeed is a tenant's whole open-req list.
//
// XMLName is declared, so a response whose root element is not <workzag-jobs>
// fails to unmarshal instead of yielding zero positions. That distinction is the
// entire point: Personio's feed is a per-tenant opt-in, and a tenant that has not
// enabled it, or a subdomain that no longer exists, answers with something that
// is not this document. Decoding that into an empty list would report the source
// as a company that is simply not hiring, which docs/architecture-roadmap.md
// calls the worst failure available.
//
// The element is named for Workzag, the company Personio was founded as. It has
// outlived the rename by more than a decade.
type personioFeed struct {
	XMLName   xml.Name           `xml:"workzag-jobs"`
	Positions []personioPosition `xml:"position"`
}

// personioPosition is one opening in the feed.
//
// Only the fields this adapter publishes are modelled, per
// docs/adding-a-source.md. The <jobDescriptions> block — the entire posting
// body, in entity-encoded HTML, and the great majority of the feed's bytes — is
// deliberately not decoded: [internal.JobPosting] has nowhere to put it, and
// skipping it keeps a 32 MiB feed from becoming 32 MiB of retained strings.
type personioPosition struct {
	// ID is Personio's own posting id, and the only thing the public posting URL
	// is built from, see [personioPostingURL].
	ID   string `xml:"id"`
	Name string `xml:"name"`

	// Office is the primary location; a tenant with several publishes the rest
	// under <additionalOffices>.
	Office           string   `xml:"office"`
	AdditionalOffice []string `xml:"additionalOffices>office"`

	Department string `xml:"department"`

	// RecruitingCategory is Personio's second, independent grouping of a posting
	// ("Sales", "Tech"). It is stored as the team rather than dropped because
	// [internal.Filter.Departments] searches department and team together, so
	// whichever of the two a tenant actually fills in answers `--department`.
	RecruitingCategory string `xml:"recruitingCategory"`

	// EmploymentType is Personio's tenure vocabulary: "permanent", "intern",
	// "trainee", "freelance", "working-student". Note that it is not the
	// full-time/part-time distinction, which is Schedule.
	EmploymentType string `xml:"employmentType"`

	// Schedule is "full-time", "part-time" or "full-or-part-time".
	Schedule string `xml:"schedule"`

	// Seniority is Personio's level vocabulary: "entry-level", "experienced",
	// "student", "lead". It is stored verbatim, which is what
	// [internal.JobPosting.Seniority] is for — levelling is a per-employer
	// ladder, and canonicalising it would be this project inventing an opinion
	// about somebody else's job architecture.
	Seniority string `xml:"seniority"`

	// CreatedAt is when the posting was created, in ISO-8601 with a numeric
	// zone. It is the only date the feed carries.
	CreatedAt string `xml:"createdAt"`
}

// personioTimeLayouts are the shapes a Personio timestamp arrives in, most
// likely first.
var personioTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// personioTime parses Personio's createdAt into UTC, returning the zero time
// when the feed published none or the value cannot be read.
//
// An unreadable value yields the zero time rather than an error: one posting with
// an odd timestamp must not cost a board its other postings, and
// [internal.Filter.PostedSince] excludes undated postings anyway, so the failure
// mode is a posting missing from a date query rather than a wrong date in it.
func personioTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}

	for _, layout := range personioTimeLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			// Stored in UTC so comparing a Personio posting with an Ashby one is a
			// comparison of instants rather than of the zones two boards happened
			// to render in — and this platform is almost entirely CET/CEST, which
			// is an hour or two off UTC all year.
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// personioLocation renders the offices a posting is offered at.
func personioLocation(position personioPosition) string {
	names := make([]string, 0, len(position.AdditionalOffice)+1)

	for _, office := range append([]string{position.Office}, position.AdditionalOffice...) {
		if office = strings.TrimSpace(office); office != "" && !slices.Contains(names, office) {
			names = append(names, office)
		}
	}

	if len(names) == 0 {
		return "unknown"
	}

	return strings.Join(names, "; ")
}

// personioEmploymentType resolves the engagement from the two fields Personio
// publishes, which split one concept in a way no other board here does.
//
// employmentType carries tenure ("permanent", "intern", "freelance") and
// schedule carries hours ("full-time", "part-time"), so both have to be
// consulted. employmentType is tried first because "intern" and "freelance" are
// the more specific answers: an internship that is also full-time is better
// filed as an internship. "permanent" is deliberately unrecognised by
// [internal.NormalizeEmploymentType] — a permanent part-time role is ordinary —
// so those postings fall through to the schedule, which is exactly the intent.
//
// "full-or-part-time" is rejected before it reaches the normalizer. It squashes
// to a string ending in "parttime", so the normalizer would read a role open to
// either as part-time, and a filter cannot tell a wrong answer from a right one.
func personioEmploymentType(position personioPosition) internal.EmploymentType {
	if employment, ok := internal.NormalizeEmploymentType(position.EmploymentType); ok {
		return employment
	}

	if personioAmbiguousSchedule(position.Schedule) {
		return internal.EmploymentTypeUnknown
	}

	if employment, ok := internal.NormalizeEmploymentType(position.Schedule); ok {
		return employment
	}

	return internal.EmploymentTypeUnknown
}

// personioAmbiguousSchedule reports whether a schedule offers full-time and
// part-time both, in which case it constrains nothing.
func personioAmbiguousSchedule(schedule string) bool {
	lowered := strings.ToLower(schedule)

	return strings.Contains(lowered, "full") && strings.Contains(lowered, "part")
}

// personioPostingURL builds the public posting page for one position.
//
// The feed carries no link of its own, so this is synthesized — the same trick
// [successFactorsApplyURL] uses, and the reason this platform costs one request
// per employer rather than one per posting. The route is the tenant's own host
// plus "/job/{id}", which is the URL every Personio career site links its own
// postings by.
func personioPostingURL(host, id string) string {
	return "https://" + host + "/job/" + id
}

// personioFeedDocument fetches and parses one tenant's feed.
//
// It does not go through [fetchJSON]: the response is XML. The body is closed
// before this returns on every path, so a failed read cannot leave a connection
// pinned for the rest of the crawl.
func personioFeedDocument(ctx context.Context, httpClient *http.Client, company, feedURL string) (*personioFeed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Personio company %q at %s: %w", company, feedURL, err)
	}

	req.Header.Set("Accept", "application/xml, text/xml, */*")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Personio for company %q at %s: %w", company, feedURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from Personio for company %q at %s: %s", company, feedURL, resp.Status)
	}

	decoder := xml.NewDecoder(io.LimitReader(resp.Body, personioMaxFeedBytes))

	// Strict off, plus the HTML entity table, because the feed is XML that
	// carries HTML: descriptions arrive entity-encoded, and a single "&nbsp;"
	// that a tenant's editor left raw is enough for a strict parser to reject the
	// whole document. That is the SuccessFactors failure in a different costume
	// (see successfactors.go, where a feed that is not quite XML has to be
	// scanned rather than parsed), and it would cost an entire employer's
	// postings rather than one description this adapter does not even read.
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	feed := new(personioFeed)

	// A truncated body — a feed larger than the limit above, or a connection cut
	// mid-document — fails here as a syntax error rather than decoding into a
	// short list of positions. Half a company's postings reported as all of them
	// is precisely the silent failure this project refuses to produce.
	if err := decoder.Decode(feed); err != nil {
		return nil, fmt.Errorf("failed to decode XML feed from Personio for company %q at %s: %w", company, feedURL, err)
	}

	return feed, nil
}

// Personio returns all of the job postings for one Personio career site, or an
// error if there was a problem making the request or reading the feed.
//
// company is the tenant's subdomain, or a full host for the tenants that publish
// on a domain other than the default; see [PersonioCompanies].
//
// There is no pagination here, deliberately: the feed answers with the tenant's
// entire open-req list, so there is no page parameter for a board to ignore and
// no loop for [pageRepeatGuard] to bound.
func Personio(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.jobs.personio.de/
	// https://$company.jobs.personio.de/xml?language=en
	// https://$company.jobs.personio.de/job/$id
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			host        = personioHost(company)
			companyName = personioCompanyName(company)

			// language=en asks for the English rendering where the operator
			// maintains one; tenants that do not keep the posting in its own
			// language, which is the honest fallback for a platform whose
			// employers are mostly German-speaking.
			feedURL = "https://" + host + "/xml?language=en"
		)

		feed, err := personioFeedDocument(ctx, httpClient, company, feedURL)
		if err != nil {
			yield(nil, err)

			return
		}

		yielded := 0

		for _, position := range feed.Positions {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			var (
				id    = strings.TrimSpace(position.ID)
				title = strings.TrimSpace(position.Name)
			)

			// Without an id there is no URL to publish, since the link is built
			// from it.
			if id == "" || title == "" {
				continue
			}

			posting := &internal.JobPosting{
				Company:  companyName,
				URL:      personioPostingURL(host, id),
				Title:    title,
				Location: personioLocation(position),

				Department:     strings.TrimSpace(position.Department),
				EmploymentType: personioEmploymentType(position),
				Seniority:      strings.TrimSpace(position.Seniority),
				PostedAt:       personioTime(position.CreatedAt),
				ExternalID:     id,
				Source: internal.PostingSource{
					Platform: personioPlatform,
					Key:      company,
				},
			}

			// Only when it says something the department does not already.
			if category := strings.TrimSpace(position.RecruitingCategory); !strings.EqualFold(category, posting.Department) {
				posting.Team = category
			}

			// Personio publishes no structured workplace field at all, so
			// WorkplaceType stays unknown and Remote stays nil. An office named
			// "Remote" is location text, and [internal.NormalizeWorkplaceType] is
			// explicit that it must not be fed one: "Remote, OR" is a town in
			// Oregon often enough that [internal.JobPosting.IsRemote]'s heuristic
			// is kept deliberately separate from a board's structured answer.
			// Leaving both empty is what lets that heuristic still run.

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// A feed full of positions that produced no postings at all means every
		// one of them was missing an id or a title, which no live board does. It
		// is the signature of a renamed element, and reporting zero postings for
		// it would be indistinguishable from a company that is not hiring.
		if len(feed.Positions) > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected feed shape from Personio for company %q at %s: %d positions decoded but none carried both an id and a name", company, feedURL, len(feed.Positions)))
		}
	}
}
