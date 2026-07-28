package services

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// brassRingPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const brassRingPlatform = "brassring"

func init() {
	registerBuiltin(brassRingPlatform, multiJobsFuncNamed(BrassRing, BrassRingGateways, brassRingCompanyName))
}

// BrassRingGateways holds the IBM/Kenexa BrassRing "Talent Gateways" this
// project crawls, one "slug,partnerid,siteid[,host]" line per gateway.
//
// docs/source-backlog.md:48 has listed BrassRing as "still missing
// (sjobs.brassring.com)" with Lockheed Martin and Home Depot hourly roles named
// as the coverage this project cannot see. This adapter closes that row: Home
// Depot alone published 23,965 postings at probe time, which no other platform
// in this binary can reach.
//
// # Tenancy is a non-guessable pair, and that is the whole difficulty
//
// A gateway is identified by a (partnerid, siteid) pair read off an employer's
// careers URL. Neither part can be derived from a company name, one partnerid
// can serve several gateways ("hobbylobby" and "mardel" share 25879; "unitypoint"
// and "unitypointmeriter" share 25790; "texastech" and "texastechfaculty" share
// 25898), and a few employers are served from krb-sjobs.brassring.com instead of
// sjobs.brassring.com. So the key is a tuple, exactly as
// [SuccessFactorsTenants] and the Oracle tenant list are, and
// [brassRingCompanyName] takes the display name from its first field.
//
// # This list is measured, not staged
//
// All 46 candidate pairs in testdata/candidates/brassring_gateways.txt were
// probed live on 2026-07-28 against ProcessSortAndShowMoreJobs; see that file's
// header for the full result. 34 answered with JobsCount above zero, and 33 of
// those are registered here. Between them they published 63,819 postings, about
// 50 per HTTP request, which makes this the second-cheapest lane per posting
// measured in this project.
//
// The one live gateway deliberately left out is "nats" (30041/5722): all six of
// its postings share the title "Trainee Air Traffic Controller" and carry
// formtext4 values of "Test", "TEST", "1 Test Fail" and "Positive Test (all
// pass)". docs/adding-a-source.md warns about exactly this — a board whose
// postings are sandbox artefacts is not a real board — and it is the only
// gateway of the 34 that fails that check.
//
// Three gateways an upstream curator had dropped as "title-only" are registered
// here: publix (26173/5197), harborfreight (26281/6657) and ukansas
// (25752/5542). That curator needed a description per posting;
// [internal.JobPosting] has nowhere to put one, so a feed that publishes title,
// link, requisition id and date is a complete source by this project's standard
// and those three add 1,952 postings for nothing.
var BrassRingGateways = []string{
	"aafes,25212,6065",
	"adm,25416,5998",
	"baesystems,25771,5464",
	"bechtel,26639,5507",
	"bestbuy,25632,5649",
	"boots,30042,5807,krb-sjobs.brassring.com",
	"bostonchildrens,368,5205",
	"dollarbank,25950,5192",
	"edwardjones,26235,5374",
	"generalatomics,25539,5313",
	"guess,25813,5079",
	"harborfreight,26281,6657",
	"highlandhospital,25940,5203",
	"hobbylobby,25879,5295",
	"homedepot,25526,5032",
	"infosys,25633,5439",
	"lmcareers,30122,6533,krb-sjobs.brassring.com",
	"mardel,25879,5298",
	"marketsource,26223,5359",
	"northropgrumman,16030,6090",
	"peacecorps,25332,5414",
	"performancefoodgroup,26350,6930",
	"publix,26173,5197",
	"srns,25264,5259",
	"teamhealth,26628,5691",
	"texastech,25898,5635",
	"texastechfaculty,25898,5637",
	"ucsf,6495,5861",
	"ukansas,25752,5542",
	"unitypoint,25790,5083",
	"unitypointmeriter,25790,5084",
	"ussteel,25307,5238",
	"walgreens,26336,5014",
}

const (
	// brassRingDefaultHost serves every gateway that does not name another one.
	brassRingDefaultHost = "sjobs.brassring.com"

	// brassRingPageSize is the number of postings asked for per request.
	//
	// The server caps this at 50 whatever is asked for: a request with
	// "pageSize":"200" against Home Depot's gateway answered with exactly 50
	// postings, byte-for-byte the same page as "pageSize":"50". Sending the real
	// cap keeps the request honest about what it expects back.
	brassRingPageSize = 50

	// brassRingMaxPages bounds one gateway's pagination.
	//
	// The loop already stops on an empty page, on a repeated page, and at the
	// page count implied by the JobsCount the first page reports — but this
	// project has been burned by boards that ignore their page parameter (see
	// [pageRepeatGuard]), and the largest gateway measured needs 480 pages, so
	// the ceiling has to be well clear of that and still finite. At 50 postings
	// a page this allows 50,000 postings from one employer.
	brassRingMaxPages = 1_000
)

// BrassRing fetches one BrassRing Talent Gateway.
//
// # The request shape
//
// A plain GET of the gateway's own careers page,
// https://<host>/TGnewUI/Search/Home/Home?partnerid=..&siteid=.., answers 200
// with about 875 KB of HTML that contains no postings — the listing is loaded
// afterwards by a POST to a JSON search endpoint, which is what this adapter
// calls:
//
//	POST https://<host>/TGnewUI/Search/Ajax/ProcessSortAndShowMoreJobs
//	{"partnerId":"..","siteId":"..","keyword":"","location":"",
//	 "activeFacetCategories":[],"selectedFacets":[],
//	 "pageNumber":"1","pageSize":"50"}
//
// It is genuinely key-free, which is the test this project applies before
// scheduling a platform at all. Measured on 2026-07-28: no API key, no
// Authorization header, no CSRF token and no cookie. Establishing a session by
// GETting the careers page first and replaying its four or five cookies returned
// byte-identical results to sending the POST cold, on every gateway tried. There
// is a second, better-known endpoint (PowerSearchJobs) that does require a token
// and a cookie; this is not that endpoint, and the difference is why BrassRing
// is reachable here at all.
//
// # Pagination
//
// pageNumber increments from 1 and pageSize is capped server-side at 50, so a
// gateway's postings arrive in a fixed, ordered sequence of pages and there is
// nothing to fan out within one source. The first page reports JobsCount, the
// gateway's total, which bounds the loop up front instead of discovering the end
// by asking one page too many.
func BrassRing(ctx context.Context, httpClient *http.Client, key string) internal.Jobs {
	// https://sjobs.brassring.com/TGnewUI/Search/Home/Home?partnerid=$partner&siteid=$site
	// https://sjobs.brassring.com/TGnewUI/Search/Ajax/ProcessSortAndShowMoreJobs
	return func(yield func(*internal.JobPosting, error) bool) {
		gateway, err := parseBrassRingGateway(key)
		if err != nil {
			yield(nil, err)

			return
		}

		var (
			pages      pageRepeatGuard
			dates      brassRingDateEvidence
			totalPages = brassRingMaxPages
			jobs       int
			yielded    int
		)

		for page := 1; page <= totalPages; page++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			results, err := brassRingPage(ctx, httpClient, gateway, page)
			if err != nil {
				yield(nil, err)

				return
			}

			listed := results.Jobs.Job

			// An empty page is the end of the gateway. It is also what a valid
			// pair with nothing published answers with on page 1, which is a
			// company that is not hiring rather than a failure.
			if len(listed) == 0 {
				break
			}

			if page == 1 {
				totalPages = brassRingPageCount(results.JobsCount, len(listed))
			}

			ids := make([]string, 0, len(listed))
			for _, job := range listed {
				ids = append(ids, job.question(brassRingRequisitionQuestion))
			}

			// A gateway that answers page N with page N-1 is not paginating, and
			// asking for page N+1 would repeat until the crawl deadline.
			if pages.repeated(ids) {
				break
			}

			dates.observe(listed)
			order := dates.order()

			for _, job := range listed {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				jobs++

				title := brassRingText(job.question(brassRingTitleQuestion))
				postingURL := strings.TrimSpace(job.Link)

				if title == "" || !strings.HasPrefix(postingURL, "https://") {
					continue
				}

				posting := &internal.JobPosting{
					Company:  gateway.slug,
					URL:      postingURL,
					Title:    title,
					Location: brassRingLocation(gateway, job),

					Department:    brassRingText(job.question(brassRingDepartmentQuestion)),
					UpdatedAt:     brassRingTime(job.question(brassRingUpdatedQuestion), order),
					RequisitionID: brassRingText(job.question(brassRingAutoReqQuestion)),
					ExternalID:    brassRingText(job.question(brassRingRequisitionQuestion)),
					Source: internal.PostingSource{
						Platform: brassRingPlatform,
						Key:      gateway.key,
					},
				}

				yielded++

				if !yield(posting, nil) {
					return
				}
			}
		}

		// Pages full of jobs that produced no postings at all means every one of
		// them was missing a title or a link, which no live gateway does. It is
		// the signature of a renamed question, and reporting zero postings for it
		// would be indistinguishable from an employer that is not hiring.
		if jobs > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from BrassRing for gateway %q at %s: %d jobs decoded but none carried both a %q question and an https link", gateway.key, gateway.searchURL(), jobs, brassRingTitleQuestion))
		}
	}
}

// The questions this adapter reads, by name.
//
// BrassRing puts every per-posting field in a flat list of {QuestionName, Value}
// pairs whose contents and order are configured per gateway, so each is looked
// up by name and never by position. Measured across the 34 live gateways on
// 2026-07-28: these four are the only names that appear on every one of them
// (alongside the fixed clientid/siteid/gqid/latitude/longitude plumbing), and
// "department" appears on seven.
const (
	brassRingTitleQuestion       = "jobtitle"
	brassRingRequisitionQuestion = "reqid"
	brassRingAutoReqQuestion     = "autoreq"
	brassRingUpdatedQuestion     = "lastupdated"
	brassRingDepartmentQuestion  = "department"
	brassRingLocationQuestion    = "location"
)

// brassRingLocationQuestions names the questions carrying a geographic location
// on the gateways whose location is not in the "location" question.
//
// This table is the honest answer to the one field BrassRing does not
// standardise. Only 7 of the 34 live gateways publish a "location" question at
// all; the rest put the location in a gateway-configured "formtextN" slot whose
// number means nothing across gateways — formtext5 is a city at General Atomics,
// a city and state at Bechtel, a whole job description at Home Depot and a
// business unit at Performance Food Group.
//
// So each entry below was read off a captured page of 50 live postings on
// 2026-07-28 and is recorded with the values that justify it. A gateway that is
// not in this table and has no "location" question publishes an empty location
// rather than a guessed one: a wrong location is worse than a missing one,
// because a filter cannot tell it apart from a right one.
//
// Two entries deliberately override a "location" question that does exist,
// because the gateway uses it for something that is not a place:
// performancefoodgroup's says "CBI Riviera (1867)" (a depot) while its formtext5
// says "Abilene, Texas (TX)", and dollarbank's says "Corporate Headquarters"
// while its formtext2 says "Pittsburgh, PA".
//
// Two more gateways were considered and left out on the same rule: "guess"
// publishes only a store name ("Tanger Outlets Atlantic City - GbG"), and
// "teamhealth" only a title with a place glued on ("Academic Nocturnist in
// Pompano Beach, FL"). Neither is a location field.
var brassRingLocationQuestions = map[string][]string{
	// "100 CALLE 12,BO QUEBRADA VUELTA,FAJARDO,PR,00738"
	"26336,5014": {"formtext22"},
	// "Camp Hill" + "PA - Pennsylvania"
	"25416,5998": {"formtext8", "formtext9"},
	// "Germany - - Ansbach", "United States - Alabama - Fort Rucker"
	"25212,6065": {"formtext3"},
	// "Norfolk" + "Virginia"
	"25771,5464": {"formtext20", "formtext31"},
	// "Port Arthur, Texas"
	"26639,5507": {"formtext5"},
	// "Everett" + "Massachusetts". This is the gateway
	// docs/research/ats-platform-survey.md records as "location field unmapped
	// (parses empty)"; it is mapped here.
	"25632,5649": {"formtext12", "formtext10"},
	// "Cleveland, OH" — overrides a "location" of "Corporate Headquarters".
	"25950,5192": {"formtext2"},
	// "Cloquet" + "Minnesota"
	"26235,5374": {"formtext61", "formtext42"},
	// "Poway" + "California"
	"25539,5313": {"formtext5", "formtext4"},
	// "ALEXANDRIA, LA, United States"
	"26281,6657": {"formtext11"},
	// "Lancaster" + "Pennsylvania"
	"25879,5295": {"formtext5", "formtext1"},
	// "Norman" + "Oklahoma" (Hobby Lobby's sister chain, same partnerid)
	"25879,5298": {"formtext5", "formtext1"},
	// "Bucharest", "Atlanta, GA", "Anywhere in the US and/or Remote"
	"25633,5439": {"formtext2"},
	// "Ampthill - Bedfordshire", "Barrow-in-Furness"
	"30122,6533": {"formtext18"},
	// "Brentwood" + "Tennessee"
	"26223,5359": {"formtext21", "formtext6"},
	// "Uganda", "Costa Rica", "Paraguay" — a Peace Corps posting is a country.
	"25332,5414": {"formtext3"},
	// "Houma, Louisiana (LA)" — overrides a "location" of "Performance Caro (0635)".
	"26350,6930": {"formtext5"},
	// "Lubbock", "Amarillo"
	"25898,5635": {"formtext11"},
	// "West Mifflin", "Gary", "Braddock"
	"25307,5238": {"formtext13"},
}

// brassRingGateway is one parsed entry of [BrassRingGateways].
type brassRingGateway struct {
	// key is the entry exactly as registered, which is what [Source.Key] and
	// [internal.PostingSource.Key] carry. Kept verbatim rather than rebuilt from
	// the parts below, so the identity a posting reports is the one a person can
	// paste back into --company.
	key string

	// slug is this project's name for the employer, and the only part of the
	// tuple a person ever types.
	slug string

	// partnerID and siteID are the ?partnerid= and ?siteid= values off the
	// gateway's careers URL. Neither can be derived from the other or from the
	// employer's name.
	partnerID string
	siteID    string

	// host is the BrassRing instance serving this gateway.
	host string
}

// searchURL is the endpoint this gateway's postings are read from.
func (g brassRingGateway) searchURL() string {
	return "https://" + g.host + "/TGnewUI/Search/Ajax/ProcessSortAndShowMoreJobs"
}

// tenancy is the (partnerid, siteid) pair, which is what
// [brassRingLocationQuestions] is keyed by. It deliberately excludes the slug,
// so renaming a gateway here cannot silently drop its location mapping.
func (g brassRingGateway) tenancy() string { return g.partnerID + "," + g.siteID }

// parseBrassRingGateway splits a "slug,partnerid,siteid[,host]" key.
//
// A malformed entry is an error rather than a best-effort guess, for the same
// reason [parseSuccessFactorsTenant] gives: the parts are independent facts
// about a gateway that cannot be derived from each other, so a short key is a
// mis-transcribed line rather than a tenant missing a default.
//
// The host is the one part that does have a real default, because all but a
// handful of gateways live on sjobs.brassring.com.
func parseBrassRingGateway(key string) (brassRingGateway, error) {
	const want = "slug,partnerid,siteid[,host]"

	parts := strings.Split(key, ",")
	if len(parts) != 3 && len(parts) != 4 {
		return brassRingGateway{}, fmt.Errorf("invalid BrassRing gateway %q: want %q", key, want)
	}

	gateway := brassRingGateway{
		key:       key,
		slug:      strings.TrimSpace(parts[0]),
		partnerID: strings.TrimSpace(parts[1]),
		siteID:    strings.TrimSpace(parts[2]),
		host:      brassRingDefaultHost,
	}

	if len(parts) == 4 {
		gateway.host = strings.TrimSpace(parts[3])
	}

	if gateway.slug == "" || gateway.partnerID == "" || gateway.siteID == "" || gateway.host == "" {
		return brassRingGateway{}, fmt.Errorf("invalid BrassRing gateway %q: want %q with every part set", key, want)
	}

	return gateway, nil
}

// brassRingCompanyName derives the display name from a gateway tuple: the slug,
// which is the first field.
//
// It returns the key unchanged when the tuple is malformed, so a bad entry stays
// traceable back to the line that produced it rather than becoming an empty name
// in the company list — the same choice [successFactorsCompanyName] makes.
func brassRingCompanyName(key string) string {
	gateway, err := parseBrassRingGateway(key)
	if err != nil {
		return key
	}

	return gateway.slug
}

// brassRingResults is one page of a gateway's search.
//
// Jobs is not a pointer here, unlike the enveloping slice on the other adapters
// in this package, because BrassRing distinguishes the two cases itself: a
// gateway with nothing to show answers with a complete envelope whose
// "Jobs":{"Job":[]} is empty and whose JobsCount is 0, and a partnerid/siteid
// pair that does not exist answers HTTP 500 with an HTML error page that
// fetchJSON rejects before this type is ever reached.
type brassRingResults struct {
	// JobsCount is the gateway's total across all pages, present on every page.
	// It is what bounds pagination; see [brassRingPageCount].
	JobsCount int `json:"JobsCount"`

	Jobs struct {
		Job []brassRingJob `json:"Job"`
	} `json:"Jobs"`
}

// brassRingJob is one posting on a gateway.
//
// Only Link and the question list are modelled. The rest of the object is
// application state for the logged-in candidate experience — Applied,
// CurrentSubmissions, SavedDate, NextApplyDate, geodist, score — none of which
// [internal.JobPosting] has anywhere to put, and all of which would be one more
// field to break when BrassRing changes it.
type brassRingJob struct {
	// Link is the public posting page. It is an absolute https URL carrying the
	// gateway's own partnerid and siteid, present and well-formed on all 1,506
	// postings captured on 2026-07-28.
	Link string `json:"Link"`

	// Questions is the per-posting field list, name-keyed. Its contents and
	// order are gateway configuration, so it is only ever read through
	// [brassRingJob.question].
	Questions []brassRingQuestion `json:"Questions"`
}

// brassRingQuestion is one name-keyed field on a posting.
type brassRingQuestion struct {
	QuestionName string `json:"QuestionName"`

	// Value arrived as a JSON string on all 21,137 questions captured on
	// 2026-07-28, but is decoded permissively: this list is gateway-configured,
	// a numeric or boolean value anywhere in it would otherwise fail the decode,
	// and fetchJSON decodes a whole page at once, so one odd field on one
	// posting would cost the gateway all 50.
	Value brassRingValue `json:"Value"`
}

// question returns the value of the named question, or "" when this posting does
// not carry it.
//
// Lookup is by name and never by index. Question order is gateway
// configuration — Walgreens sends reqid, hotjob, clientid, siteid, gqid,
// jobreqlanguage, latitude, longitude, lastupdated, jobtitle, autoreq,
// formtext22, formtext27, while Home Depot sends a different set in a different
// order — so an adapter that read by position would silently publish one field's
// value under another field's name.
func (j brassRingJob) question(name string) string {
	for _, question := range j.Questions {
		if question.QuestionName == name {
			return question.Value.String()
		}
	}

	return ""
}

// brassRingValue decodes a question value whose JSON type BrassRing does not
// promise to hold stable.
type brassRingValue struct {
	value string
}

// String returns the value as text.
func (v brassRingValue) String() string { return v.value }

// UnmarshalJSON accepts a string, a number, a boolean, or null.
func (v *brassRingValue) UnmarshalJSON(data []byte) error {
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	switch typed := decoded.(type) {
	case nil:
		v.value = ""
	case string:
		v.value = typed
	case float64:
		v.value = strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		v.value = strconv.FormatBool(typed)
	default:
		// An object or an array is not a value this adapter can render, and
		// guessing at one would be worse than reporting nothing.
		v.value = ""
	}

	return nil
}

// brassRingPage fetches one page of a gateway's postings.
//
// It is a function rather than an inline call so the response body is closed per
// request. A deferred Close inside the pagination loop is what parked every
// large Workday tenant on its limiter until the request context expired, see
// httpx's defaultSlotWaitWarn.
func brassRingPage(ctx context.Context, httpClient *http.Client, gateway brassRingGateway, page int) (*brassRingResults, error) {
	body, err := json.Marshal(map[string]any{
		"partnerId":             gateway.partnerID,
		"siteId":                gateway.siteID,
		"keyword":               "",
		"location":              "",
		"activeFacetCategories": []string{},
		"selectedFacets":        []string{},
		"pageNumber":            strconv.Itoa(page),
		"pageSize":              strconv.Itoa(brassRingPageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build BrassRing search body for gateway %q: %w", gateway.key, err)
	}

	return fetchJSON[brassRingResults](ctx, httpClient, "BrassRing", gateway.key, jsonRequest{
		URL:    gateway.searchURL(),
		Method: http.MethodPost,
		Body:   string(body),
	})
}

// brassRingPageCount returns how many pages a gateway's JobsCount implies.
//
// Bounding the loop from the total the first page reports is what keeps the
// common case at exactly the number of requests the postings need, instead of
// always spending one extra request to discover the end. The result is clamped
// to [brassRingMaxPages] because JobsCount is a number a third party controls.
func brassRingPageCount(total, perPage int) int {
	if perPage <= 0 || total <= 0 {
		return 1
	}

	pages := (total + perPage - 1) / perPage
	if pages > brassRingMaxPages {
		return brassRingMaxPages
	}

	return pages
}

// brassRingLocation renders a posting's location.
//
// The gateway's own "location" question is used when it has one, and
// [brassRingLocationQuestions] overrides that for the gateways where it is
// mapped. A gateway in neither case returns "", which is the deliberate outcome
// documented on that table.
func brassRingLocation(gateway brassRingGateway, job brassRingJob) string {
	names, ok := brassRingLocationQuestions[gateway.tenancy()]
	if !ok {
		names = []string{brassRingLocationQuestion}
	}

	parts := make([]string, 0, len(names))

	for _, name := range names {
		if part := brassRingText(job.question(name)); part != "" {
			parts = append(parts, part)
		}
	}

	return strings.Join(parts, ", ")
}

// brassRingText cleans one question value.
//
// Values are HTML fragments rather than plain text: they carry entities
// ("Logistics &amp; Transportation", "&nbsp;") and are consistently padded with
// a trailing space by the runtime. Unescaping is one pass rather than repeated,
// so a title that really contains "&amp;amp;" is not mangled into "&".
func brassRingText(value string) string {
	if value == "" {
		return ""
	}

	unescaped := html.UnescapeString(value)

	// U+00A0 arrives both literally and as &nbsp;, and a title ending in one
	// would otherwise survive TrimSpace on some platforms and not others.
	unescaped = strings.ReplaceAll(unescaped, " ", " ")

	return strings.Join(strings.Fields(unescaped), " ")
}

// brassRingDateOrder says how a gateway writes a slash-separated date.
type brassRingDateOrder int

const (
	// brassRingDateAmbiguous means the page gave no evidence either way, so no
	// slash-separated date on it is parsed at all.
	brassRingDateAmbiguous brassRingDateOrder = iota
	brassRingDateMonthFirst
	brassRingDateDayFirst
)

// brassRingDateEvidence accumulates, over one gateway's pages, which way round
// that gateway writes a slash-separated date.
//
// BrassRing writes "lastupdated" three different ways, all measured on
// 2026-07-28: "27-Jul-2026" on most gateways, "07/27/2026" on Home Depot, GUESS,
// Harbor Freight, Infosys and Texas Tech, and "27/07/2026" on the UK gateway at
// krb-sjobs. The first is unambiguous. The other two are the same eight
// characters meaning different dates, and nothing in the response says which.
//
// Rather than guess from the host or the employer's country, the order is
// inferred from the gateway's own data: a date whose first number exceeds 12 can
// only be day-first, one whose second number exceeds 12 can only be month-first.
//
// The evidence is deliberately kept for the whole source rather than judged one
// page at a time, and that is not a detail. Measured on Home Depot's 22,751
// postings: per page, only 53% got a date, because BrassRing sorts by date so a
// page's fifty postings routinely share one value — page 100 was fifty copies of
// "11/07/2025" and page 450 fifty copies of "11/09/2016", neither of which
// settles anything on its own. Page 1 settles this gateway as month-first, and
// carrying that forward dates the rest.
//
// A gateway that never produces evidence, or that contradicts itself, leaves its
// slash dates unset: an absent UpdatedAt is visibly absent, while a date read the
// wrong way round is silently wrong for eleven months of the year.
type brassRingDateEvidence struct {
	monthFirst bool
	dayFirst   bool
}

// observe folds one page of jobs into the evidence.
func (e *brassRingDateEvidence) observe(jobs []brassRingJob) {
	for _, job := range jobs {
		first, second, ok := brassRingSlashParts(job.question(brassRingUpdatedQuestion))
		if !ok {
			continue
		}

		if first > 12 {
			e.dayFirst = true
		}

		if second > 12 {
			e.monthFirst = true
		}
	}
}

// order reports what the evidence so far says.
func (e brassRingDateEvidence) order() brassRingDateOrder {
	switch {
	case e.monthFirst && !e.dayFirst:
		return brassRingDateMonthFirst
	case e.dayFirst && !e.monthFirst:
		return brassRingDateDayFirst
	default:
		return brassRingDateAmbiguous
	}
}

// brassRingSlashParts splits "07/27/2026" into its first two numbers.
func brassRingSlashParts(value string) (first, second int, ok bool) {
	fields := strings.Split(strings.TrimSpace(value), "/")
	if len(fields) != 3 {
		return 0, 0, false
	}

	first, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}

	second, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}

	return first, second, true
}

// brassRingTime parses a "lastupdated" value.
//
// The result lands on [internal.JobPosting.UpdatedAt] and never on PostedAt.
// BrassRing publishes no first-published date anywhere in this response, and
// editing a requisition does not make an old one new — filling PostedAt from
// this would quietly fill every "posted this week" query with stale postings.
//
// An unparseable value yields the zero time rather than an error: a posting with
// an unreadable date is still a posting, and the zero value already means "the
// board did not say".
func brassRingTime(value string, order brassRingDateOrder) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	if parsed, err := time.Parse("02-Jan-2006", value); err == nil {
		return parsed.UTC()
	}

	layout := ""

	switch order {
	case brassRingDateMonthFirst:
		layout = "01/02/2006"
	case brassRingDateDayFirst:
		layout = "02/01/2006"
	case brassRingDateAmbiguous:
		return time.Time{}
	}

	parsed, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}
	}

	return parsed.UTC()
}
