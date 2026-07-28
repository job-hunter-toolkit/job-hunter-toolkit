package services

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"golang.org/x/net/html"
)

// icimsPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
//
// "icims" here means the classic server-rendered career portal on
// {host}.icims.com, which docs/source-backlog.md calls "iCIMS proper (no Jibe
// wrapper)". iCIMS's modern template is Jibe, and this project has crawled 251
// of those boards since #34 under the separate [jibePlatform] key. The two are
// one vendor and two entirely different wire formats: a Jibe board answers
// /api/jobs with JSON, and every host registered here 404s that path.
const icimsPlatform = "icims"

func init() {
	registerBuiltin(icimsPlatform, multiJobsFuncNamed(ICIMS, ICIMSHosts, icimsCompanyName))

	// Every tenant is a subdomain of one vendor domain, so this is the
	// bamboohr.com / pinpointhq.com shape: left on the generic exact-host policy
	// each of the 63 hosts would get its own four-slot limiter and this platform
	// alone could put 252 concurrent requests on one backend. That is the shape
	// that rate-limited 56 Workable boards into looking dead.
	//
	// Registering the hosts from here rather than adding a `.icims.com` suffix
	// arm to httpx.servicePolicyFor keeps one list. The suffix arm is still the
	// better home for it -- it would also cover any host promoted out of
	// testdata/candidates/icims_hosts.txt without a second edit -- and is
	// reported as a wanted httpx change rather than made here.
	httpx.RegisterSharedBackend(icimsPlatform, ICIMSHosts...)
}

const (
	// icimsMaxPages bounds a single tenant's pagination walk, unconditionally.
	//
	// The termination signal this adapter actually uses is the board's own
	// <link rel="next">, which iCIMS omits on the last page and which was
	// correct on all 778 page requests measured on 2026-07-28: every one of the
	// 70 boards walked ended by itself, none by a bound. This exists for the
	// case where that signal is wrong, because this project has already paid for
	// an HTML pagination loop that trusted the board: a board ignoring its page
	// parameter produced 500,001 duplicate postings in under a second, and
	// [pageRepeatGuard] ends that one request sooner but only when the repeated
	// page is byte-identical in its ids.
	//
	// 400 pages is roughly five times the deepest walk measured
	// (jobs-selectmedicalcorp, 3,809 postings in 77 requests) and covers 8,000
	// postings on a 20-per-page tenant or 20,000 on a 50-per-page one.
	icimsMaxPages = 400

	// icimsJobPath is the path prefix every posting anchor on a classic portal
	// uses: /jobs/{id}/{title-slug}/job.
	//
	// Required, and it is what keeps this adapter's URLs on the tenant's own
	// host. An apply URL pointing at another ATS is the single mistake that
	// caused every double count found in this repo, so the anchor's host is
	// checked against the tenant's before a posting is yielded.
	icimsJobPath = "/jobs/"
)

// ICIMSHosts holds the iCIMS classic career portals this project crawls, one
// public hostname per entry.
//
// The host is the key rather than a slug because iCIMS slugs are not derivable:
// docs/research/ats-platform-survey.md measured that guessing careers-{company}
// hits about 1 in 40, and only ~57% of hosts use the "careers-" prefix at all.
// The 63 below carry eight distinct prefixes.
//
// # This list is measured, not staged
//
// Every host answered a live walk on 2026-07-28. 321 hosts from the 2,053-entry
// candidate file at testdata/candidates/icims_hosts.txt were probed at
// https://{host}/jobs/search?pr=0&in_iframe=1: 306 answered 200 with at least
// one iCIMS_JobCardItem, 12 answered 200 with none, and 3 answered 404. 70 of
// the 306 were then walked to the last page -- 778 HTTP requests, 19,922
// distinct posting URLs -- and the 63 registered here are those 70 minus the
// seven the next section explains. Each entry's cost and size is a number
// rather than an estimate:
//
//	63 boards, 649 HTTP requests, 15,932 distinct posting URLs
//	= 24.5 postings per request
//
// That number is the reason this platform is registered at all.
// docs/research/ats-platform-survey.md put iCIMS-classic among the lanes that
// "will blow the time budget" and estimated any JSON-LD detail-walk route
// "below 1 posting per request". The estimate is right about the route it
// describes -- sitemap.xml plus schema.org JSON-LD per job page really is one
// request per posting -- and wrong about this one. 25.6 puts the classic search
// route above Teamtailor (~14), Personio (~10), ADP (~8) and Paylocity (~2) on
// the survey's own ranking, and just above Pinpoint (~21).
//
// The remaining 236 hosts that answered page 0 are deliberately left staged. An
// unwalked host is an unknown quantity in both directions: jobs-selectmedicalcorp
// turned out to publish 3,809 postings, which is a third of the largest staged
// Oracle tenant and not something to discover during a nightly crawl.
//
// # Seven walked boards this project already crawls through Jibe
//
// iCIMS owns Jibe, and the same employer can run a classic portal and a Jibe
// board at once. Seven of the 70 walked hosts turned out to be exactly that,
// and all seven are deliberately absent from the list below. Measured
// 2026-07-28 by crawling both routes with this project's own adapters and
// comparing posting URLs and lowercased titles:
//
//	employer          icims          jibe           shared URLs  shared titles
//	guard             37  (34 t)     37  (34 t)     0            34 of 34
//	medicalsolutions  15  (15 t)     15  (15 t)     0            15 of 15
//	pittohio          81  (30 t)     202 (114 t)    0            30 of 30
//	peraton           1,488 (1,219)  1,533 (1,249)  0            1,207 of 1,219
//	emory             911 (544 t)    1,933 (1,298)  0            528 of 544
//	gdms              565 (437 t)    692 (478 t)    0            133 of 437
//	noodles           921 (3 t)      957 (7 t)      0            3 of 3
//
// Zero shared URLs with near-total shared titles is the signature
// docs/dedupe-audit.md calls a double count: the same opening, reachable under
// two different URLs, which [internal.Dedupe] cannot collapse because it keys on
// URL. Registering these would have added 3,990 postings to the trend line that
// are already in it.
//
// The Jibe route is kept in every case and is the better one on both axes: it
// returned more postings on six of the seven (equal on the seventh) and it costs
// far fewer requests, ~92 postings per request against this platform's 24.5.
//
// Note what did NOT settle this. Comparing title+location pairs, which is what
// docs/dedupe-audit.md usually compares, found ZERO shared pairs on all seven
// including guard, where both routes publish the same 37 postings. The two
// systems format a location differently for the same req -- "US-PA-Wilkes
// Barre" on the classic portal against "Wilkes-Barre, PA" on Jibe -- so the
// pair test reports a clean split for boards that are literal mirrors. Titles
// were the signal that worked here.
//
// # Not registered on purpose
//
// Costco, which docs/source-backlog.md names as a confirmed iCIMS employer, is
// already crawled by the Jibe adapter at careers.costco.com and is not added
// here. Charles Schwab (career-schwab.icims.com, 50 postings on page 0) answered
// and is genuinely new, but was in the probe batch rather than the walked batch;
// it stays staged with a measured page-0 count next to it rather than being
// registered on a single page.
var ICIMSHosts = []string{
	"careers-actalentservices.icims.com",
	"careers-ahmchealth.icims.com",
	"careers-allnativegroup.icims.com",
	"careers-amtrustgroup.icims.com",
	"careers-appliedsystems.icims.com",
	"careers-atlas-aerospace.icims.com",
	"careers-bancorpbank.icims.com",
	"careers-banknewport.icims.com",
	"careers-berkley.icims.com",
	"careers-bostonpizza.icims.com",
	"careers-centersusa.icims.com",
	"careers-cfins.icims.com",
	"careers-containerstore.icims.com",
	"careers-coraltreehospitality.icims.com",
	"careers-cpicardgroup.icims.com",
	"careers-eaglebankcorp.icims.com",
	"careers-eaglepicher.icims.com",
	"careers-eastwestbank.icims.com",
	"careers-federatedinsurance.icims.com",
	"careers-gd-ots.icims.com",
	"careers-globalcu.icims.com",
	"careers-hexagonpositioning.icims.com",
	"careers-hhmlp.icims.com",
	"careers-hhsys.icims.com",
	"careers-idirectgov.icims.com",
	"careers-iqt.icims.com",
	"careers-jobyaviation.icims.com",
	"careers-kiscoseniorliving.icims.com",
	"careers-knowledgeservices.icims.com",
	"careers-lmi.icims.com",
	"careers-lowesfoods.icims.com",
	"careers-magaero.icims.com",
	"careers-michiganfirst.icims.com",
	"careers-mpi.icims.com",
	"careers-nv5.icims.com",
	"careers-oceanbank.icims.com",
	"careers-omnisource.icims.com",
	"careers-petsuppliesplus.icims.com",
	"careers-pistongroup.icims.com",
	"careers-preshomes.icims.com",
	"careers-qinetiqus.icims.com",
	"careers-reynoldsconsumerproducts.icims.com",
	"careers-roadrunnersports.icims.com",
	"careers-sscgp.icims.com",
	"careers-stapharma.icims.com",
	"careers-techcu.icims.com",
	"careers-teksynap.icims.com",
	"careers-tfghospitality.icims.com",
	"careers-treliant.icims.com",
	"careers-uhnjcareers.icims.com",
	"careers-uwcu.icims.com",
	"careers-wafd.icims.com",
	"careers-wilson.icims.com",
	"careers-winco.icims.com",
	"careers-wow.icims.com",
	"corporatecareers-thefreshmarket.icims.com",
	"externalhourly-omnihotels.icims.com",
	"fieldhourly-thefreshmarket.icims.com",
	"hospital-midlandhealth.icims.com",
	"jobs-express.icims.com",
	"jobs-selectmedicalcorp.icims.com",
	"management-davidsonhospitality.icims.com",
	"storecareers-gpminvestments.icims.com",
}

// icimsAudiencePrefixes are the leading host-label segments that name a slice of
// an employer's hiring rather than the employer.
//
// Only the two generic words are listed, and only for a SECOND strip. The first
// segment is always dropped, because on every host measured it is an audience or
// role label: "clinical-emory", "fieldhourly-thefreshmarket",
// "storecareers-gpminvestments", "management-davidsonhospitality",
// "hospital-midlandhealth". Dropping only the first segment is wrong for the
// three-part shapes in the wider candidate file -- "internal-careers-rivian"
// would become "careers-rivian" and "manufacturing-jobs-marvin" would become
// "jobs-marvin", both of which hide the employer from `--company rivian` -- and
// this second pass is what handles them.
//
// It deliberately stops there. "careers-gd-ots" and "careers-atlas-aerospace"
// are employer names that contain a hyphen, so a rule that kept stripping would
// eat them.
var icimsAudiencePrefixes = map[string]bool{
	"careers": true,
	"career":  true,
	"jobs":    true,
	"job":     true,
}

// icimsCompanyName derives a readable company name from an iCIMS host.
//
// The whole host is the key, and a key like
// "corporatecareers-thefreshmarket.icims.com" sorts under "c" in the user-facing
// company list and does not match `--company thefreshmarket` in a way anyone
// would predict. The two Fresh Market boards intentionally reduce to the same
// name: they are one employer's corporate and field boards, and naming them
// alike is the truthful outcome rather than a collision to be avoided.
func icimsCompanyName(host string) string {
	label := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".icims.com")

	segments := strings.Split(label, "-")
	if len(segments) > 1 {
		segments = segments[1:]
	}

	if len(segments) > 1 && icimsAudiencePrefixes[segments[0]] {
		segments = segments[1:]
	}

	return strings.Join(segments, "-")
}

// icimsSearchURL is the classic portal's job list for one page.
//
// pr is zero-based. in_iframe=1 is the parameter iCIMS's own embed uses, and it
// is what strips the surrounding chrome: without it the same page is served
// wrapped in the tenant's branded shell, which is several times the bytes for
// exactly the same job cards.
func icimsSearchURL(host string, page int) string {
	return "https://" + host + "/jobs/search?pr=" + strconv.Itoa(page) + "&in_iframe=1"
}

// icimsAttr returns an element's attribute value.
func icimsAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}

	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}

	return ""
}

// icimsHasClass reports whether an element's class list contains token.
func icimsHasClass(n *html.Node, token string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}

	for _, field := range strings.Fields(icimsAttr(n, "class")) {
		if field == token {
			return true
		}
	}

	return false
}

// icimsText renders an element's visible text with runs of whitespace collapsed.
func icimsText(n *html.Node) string {
	var builder strings.Builder

	var walk func(*html.Node)

	walk = func(n *html.Node) {
		if n == nil {
			return
		}

		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(n)

	return strings.Join(strings.Fields(builder.String()), " ")
}

// icimsFindAll returns every element under root satisfying match, in document
// order.
func icimsFindAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var (
		found []*html.Node
		walk  func(*html.Node)
	)

	walk = func(n *html.Node) {
		if n == nil {
			return
		}

		if n.Type == html.ElementNode && match(n) {
			found = append(found, n)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(root)

	return found
}

// icimsNextElement returns the next sibling that is an element, skipping the
// whitespace text nodes the template emits between them.
func icimsNextElement(n *html.Node) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}

	return nil
}

// icimsField is one label/value pair from a job card.
//
// Value and Title are separate because the card publishes both for a date: the
// visible text is "23 minutes ago" and the span's title attribute carries
// "7/28/2026 2:40 PM". Reading the text would publish prose into a
// [time.Time]-shaped field; the attribute is the machine-readable copy that was
// on the wire all along.
type icimsField struct {
	Label string
	Value string
	Title string
}

// icimsCardFields collects every label/value pair on one job card.
//
// A card publishes them in two different shapes and this reads both, because
// which shape carries the location varies by tenant:
//
//   - a screen-reader label followed by its value span, which is how the header
//     row publishes Job Locations and Posted Date;
//   - a <dt class="iCIMS_JobHeaderField"> followed by its
//     <dd class="iCIMS_JobHeaderData">, which is how the configurable field
//     block publishes Category, Requisition ID, Job Type and pay.
//
// Labels are tenant-configured English and vary accordingly: the same date field
// is "Posted Date" on corporatecareers-thefreshmarket and "Job Post
// Information* : Posted Date" on careers-tfghospitality, and the title field is
// "Title", "Job Title", "Advertised Job Title", "Job Posting Title" and
// "Requisition Title" across five tenants measured. Every consumer below
// therefore matches on a substring of the lowercased label, never on equality.
func icimsCardFields(card *html.Node) []icimsField {
	var fields []icimsField

	for _, label := range icimsFindAll(card, func(n *html.Node) bool {
		return n.Data == "span" && icimsHasClass(n, "field-label")
	}) {
		value := icimsNextElement(label)
		if value == nil {
			continue
		}

		fields = append(fields, icimsField{
			Label: icimsText(label),
			Value: icimsText(value),
			Title: strings.TrimSpace(icimsAttr(value, "title")),
		})
	}

	for _, term := range icimsFindAll(card, func(n *html.Node) bool {
		return n.Data == "dt" && icimsHasClass(n, "iCIMS_JobHeaderField")
	}) {
		definition := icimsNextElement(term)
		if definition == nil || definition.Data != "dd" {
			continue
		}

		fields = append(fields, icimsField{
			Label: icimsText(term),
			Value: icimsText(definition),
		})
	}

	return fields
}

// icimsFirstField returns the first field whose lowercased label contains any of
// wants and none of avoid.
//
// The ordering of wants is the priority: callers list the most specific label
// first, so a card carrying both "Job Category" and "Division" answers with the
// category.
func icimsFirstField(fields []icimsField, wants []string, avoid ...string) (icimsField, bool) {
	for _, want := range wants {
		for _, field := range fields {
			lowered := strings.ToLower(field.Label)

			if !strings.Contains(lowered, want) {
				continue
			}

			skip := false

			for _, unwanted := range avoid {
				if strings.Contains(lowered, unwanted) {
					skip = true

					break
				}
			}

			if !skip {
				return field, true
			}
		}
	}

	return icimsField{}, false
}

// icimsLocationLabels are the label substrings that carry where a job is, most
// specific first.
//
// "address" is excluded rather than ranked last: careers-petsuppliesplus
// publishes "Location : Address" holding a street address ("1500 E Court St"),
// which is not a location any filter in this project can use and would displace
// the "US-TX-Seguin" the same card also carries.
var icimsLocationLabels = []string{"job location", "campus location", "location"}

// icimsDepartmentLabels are the label substrings that carry the org unit, most
// specific first. clinical-emory publishes both "Job Category" ("Nursing") and
// "Division" ("Emory Univ Hosp-Midtown"), and the category is the department.
var icimsDepartmentLabels = []string{"job category", "category", "department", "job family", "division"}

// icimsEmploymentLabels are the label substrings that carry the full-time /
// part-time distinction. Values are passed through
// [internal.NormalizeEmploymentType], so an unrecognised spelling such as
// career-schwab's "Regular" leaves the field empty rather than guessing.
var icimsEmploymentLabels = []string{"position type", "employment type", "job type", "employment status"}

// icimsRequisitionLabels are the label substrings that carry the employer's own
// requisition number, most specific first.
//
// The values are the employer's, not iCIMS's: careers-gdms publishes ID
// "2026-73835" for the posting whose URL id is 73835, and careers-wow publishes
// Job ID "2026-10493" for URL id 10493. The URL id is [internal.JobPosting.ExternalID];
// this is [internal.JobPosting.RequisitionID].
var icimsRequisitionLabels = []string{"requisition id", "requisition number", "job number", "job id", "req id", "id"}

// No compensation is published from an iCIMS card, and that is a decision
// rather than an oversight.
//
// A pay field exists and some tenants fill it in. All 70 walked boards were
// scanned on 2026-07-28 for a card field whose label mentions salary, pay,
// compensation, wage or rate. Five tenants have one, and between them the five
// strings are five different things:
//
//	careers-gdms                      Combined Salary Range  USD $82,015.00 - USD $88,743.00 /Yr.
//	careers-reynoldsconsumerproducts  Pay Range              USD $66,000.00 - USD $90,750.00 /A
//	careers-medicalsolutions          Posted Max Pay Rate    USD $70,304.00/Yr.
//	careers-winco                     Pay Range:             Starting from USD $15.00/Hr.
//	careers-uhnjcareers               Salary Range           Salary Negotiable
//
// Two of those five, careers-gdms and careers-medicalsolutions, are among the
// seven left to Jibe above, so three of the 63 registered boards publish pay.
// One is a range, one is a range whose period token is truncated ("/A"), one is
// explicitly a maximum, one is explicitly a minimum, and one is not a number.
// Reading them with a single rule publishes $70,304 as somebody's floor and $15
// as somebody's ceiling, and a wrong salary is indistinguishable from a right
// one at a glance -- which is the whole reason [internal.Provenance] exists.
//
// Two further things were measured and both argue for leaving it:
//
//   - [internal.ParseCompensationFromText] drops the upper bound of the exact
//     format the largest of these tenants uses. Given "Combined Salary Range:
//     USD $100,000.00 - USD $165,000.00 /Yr." it returns Min 100000 and Max 0,
//     because its range pattern does not expect the currency marker to be
//     repeated on the second bound. That is a defect in a shared file and is
//     reported rather than worked around here.
//   - careers-gdms published "USD $94,388.00 - USD $90,311.00 /Yr." on one of
//     the 20 cards on its first page: a range whose maximum is below its
//     minimum. Whatever reads these has to decide what that means, and this
//     adapter is not the place to guess.
//
// So the field is left on the wire, recorded here so the next person starts from
// the strings rather than from a search, and [internal.JobPosting.Compensation]
// stays nil on this platform.

// icimsPostedLabels are the label substrings that carry the publication date.
var icimsPostedLabels = []string{"posted date", "posted"}

// icimsJobIDFromURL returns the numeric posting id from a classic portal URL,
// which is the {id} in /jobs/{id}/{slug}/job.
func icimsJobIDFromURL(rawURL string) string {
	_, rest, ok := strings.Cut(rawURL, icimsJobPath)
	if !ok {
		return ""
	}

	id, _, _ := strings.Cut(rest, "/")

	if id == "" {
		return ""
	}

	for _, r := range id {
		if r < '0' || r > '9' {
			return ""
		}
	}

	return id
}

// icimsCanonicalURL turns a posting anchor's href into the tenant's own posting
// URL, or reports false when the anchor does not point at one.
//
// Two things are checked and both are load-bearing. The host must be the
// tenant's own: yielding an apply URL that points at another ATS is the single
// mistake that caused every double count found in this repo, and iCIMS cards on
// some tenants carry secondary anchors. The query string is dropped because the
// only parameter on it is in_iframe=1, the embed flag this crawler adds itself
// -- leaving it on would publish links that render without the employer's
// chrome, and would make the same opening a different URL to [internal.Dedupe]
// than the one a job seeker's browser produces.
func icimsCanonicalURL(host, href string) (string, bool) {
	href = strings.TrimSpace(href)

	prefix := "https://" + strings.ToLower(host) + icimsJobPath
	if !strings.HasPrefix(strings.ToLower(href), prefix) {
		return "", false
	}

	canonical, _, _ := strings.Cut(href, "?")
	canonical, _, _ = strings.Cut(canonical, "#")

	if icimsJobIDFromURL(canonical) == "" {
		return "", false
	}

	return canonical, true
}

// icimsNextPage returns the page number the board says comes next.
//
// iCIMS publishes it as <link rel="next" href=".../jobs/search?pr=N&in_iframe=1">
// in the document head, and omits the element entirely on the last page -- the
// template even carries the comment "don't use rel='next' if we're on last
// page". It was correct on all 778 page requests measured on 2026-07-28: every
// one of the 70 boards walked ended by itself.
//
// Trusting it is still not the same as being bounded by it, which is why the
// returned page number is required to be strictly greater than the current one
// and why [icimsMaxPages] holds regardless.
func icimsNextPage(doc *html.Node, page int) (int, bool) {
	for _, link := range icimsFindAll(doc, func(n *html.Node) bool {
		return n.Data == "link" && strings.EqualFold(icimsAttr(n, "rel"), "next")
	}) {
		href := icimsAttr(link, "href")

		_, rest, ok := strings.Cut(href, "pr=")
		if !ok {
			continue
		}

		digits, _, _ := strings.Cut(rest, "&")

		next, err := strconv.Atoi(strings.TrimSpace(digits))
		if err != nil || next <= page {
			continue
		}

		return next, true
	}

	return 0, false
}

// icimsDateOrder is how a tenant orders the day and month in the timestamp it
// puts in a posted-date span's title attribute.
type icimsDateOrder int

const (
	// icimsDateOrderUnknown means no card on this board has disambiguated the
	// two, so no date is published for it. Guessing would silently mislabel a
	// board: 3/7/2026 is five months wrong if the guess is backwards.
	icimsDateOrderUnknown icimsDateOrder = iota

	// icimsDateOrderMonthFirst is "7/28/2026 2:40 PM", which is what every US
	// tenant measured emits.
	icimsDateOrderMonthFirst

	// icimsDateOrderDayFirst is "23/07/2026 08:17", which is what
	// careers-tfghospitality emits.
	icimsDateOrderDayFirst
)

// icimsDateEvidence infers a board's date order from the whole board rather than
// from one card.
//
// The same problem, and the same solution, as [brassRingDateEvidence]: a card
// publishes 7/28/2026 or 23/07/2026 with nothing saying which is which, and the
// ambiguous majority (3/7/2026) is only resolvable by looking at cards where one
// of the two numbers exceeds 12. A board with an AM/PM marker is settled
// immediately -- iCIMS emits a 24-hour clock in the day-first locale and a
// 12-hour one in the month-first locale on every tenant measured -- and
// otherwise the evidence accumulates across the page.
//
// The zero value is ready to use.
type icimsDateEvidence struct {
	monthFirst bool
	dayFirst   bool
}

// observe records what one timestamp says about the board's date order.
func (e *icimsDateEvidence) observe(value string) {
	first, second, ok := icimsSlashParts(value)
	if !ok {
		return
	}

	switch {
	case first > 12:
		e.dayFirst = true
	case second > 12:
		e.monthFirst = true
	}

	if strings.Contains(strings.ToUpper(value), " AM") || strings.Contains(strings.ToUpper(value), " PM") {
		e.monthFirst = true
	}
}

// order reports the board's date order, or [icimsDateOrderUnknown] when the
// evidence is absent or contradictory.
func (e icimsDateEvidence) order() icimsDateOrder {
	switch {
	case e.monthFirst && !e.dayFirst:
		return icimsDateOrderMonthFirst
	case e.dayFirst && !e.monthFirst:
		return icimsDateOrderDayFirst
	default:
		return icimsDateOrderUnknown
	}
}

// icimsSlashParts splits the leading "N/N/NNNN" of a timestamp.
func icimsSlashParts(value string) (first, second int, ok bool) {
	date, _, _ := strings.Cut(strings.TrimSpace(value), " ")

	parts := strings.Split(date, "/")
	if len(parts) != 3 {
		return 0, 0, false
	}

	a, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}

	b, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}

	if _, err := strconv.Atoi(parts[2]); err != nil {
		return 0, 0, false
	}

	return a, b, true
}

// icimsMonthFirstLayouts and icimsDayFirstLayouts are the timestamp shapes
// measured on 2026-07-28, most complete first. The date-only forms are there
// because an employer-set field such as career-schwab's "Application deadline"
// carries "8/7/2026" with no clock.
var (
	icimsMonthFirstLayouts = []string{"1/2/2006 3:04 PM", "1/2/2006 15:04", "1/2/2006"}
	icimsDayFirstLayouts   = []string{"2/1/2006 15:04", "2/1/2006 3:04 PM", "2/1/2006"}
)

// icimsTime parses a posted-date timestamp under the board's own date order,
// returning the zero time when the order is unknown or the value is not a
// timestamp.
//
// The result is UTC because iCIMS publishes no zone at all: the card says
// "7/28/2026 2:40 PM" and nothing else. Storing it as UTC is what
// [internal.JobPosting.PostedAt] documents every adapter to do, and the residual
// error is bounded by one day, which is the resolution --posted-since works at.
func icimsTime(value string, order icimsDateOrder) time.Time {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return time.Time{}
	}

	var layouts []string

	switch order {
	case icimsDateOrderMonthFirst:
		layouts = icimsMonthFirstLayouts
	case icimsDateOrderDayFirst:
		layouts = icimsDayFirstLayouts
	case icimsDateOrderUnknown:
		return time.Time{}
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// icimsCard turns one job card into a posting, or reports false when the card
// carries no anchor pointing at a posting on this tenant's host.
func icimsCard(host string, card *html.Node, order icimsDateOrder) (*internal.JobPosting, bool) {
	var (
		postingURL string
		title      string
	)

	for _, anchor := range icimsFindAll(card, func(n *html.Node) bool {
		return n.Data == "a" && icimsAttr(n, "href") != ""
	}) {
		canonical, ok := icimsCanonicalURL(host, icimsAttr(anchor, "href"))
		if !ok {
			continue
		}

		// The anchor wraps a screen-reader label and an <h3> holding the title.
		// The whole anchor's text would prepend that label, so the heading is
		// preferred and the anchor's own title attribute is the fallback; it
		// carries "18075 - Groomer", which is the id and the title joined.
		postingURL = canonical

		if headings := icimsFindAll(anchor, func(n *html.Node) bool { return n.Data == "h3" }); len(headings) > 0 {
			title = icimsText(headings[0])
		}

		if title == "" {
			if _, after, ok := strings.Cut(icimsAttr(anchor, "title"), " - "); ok {
				title = strings.TrimSpace(after)
			}
		}

		break
	}

	if postingURL == "" || title == "" {
		return nil, false
	}

	fields := icimsCardFields(card)

	posting := &internal.JobPosting{
		Company:  icimsCompanyName(host),
		URL:      postingURL,
		Title:    title,
		Location: "unknown",

		ExternalID: icimsJobIDFromURL(postingURL),
		Source: internal.PostingSource{
			Platform: icimsPlatform,
			Key:      host,
		},
	}

	if field, ok := icimsFirstField(fields, icimsLocationLabels, "address"); ok && field.Value != "" {
		posting.Location = field.Value
	}

	if field, ok := icimsFirstField(fields, icimsDepartmentLabels); ok {
		posting.Department = field.Value
	}

	if field, ok := icimsFirstField(fields, icimsRequisitionLabels); ok {
		posting.RequisitionID = field.Value
	}

	if field, ok := icimsFirstField(fields, icimsEmploymentLabels); ok {
		if employment, ok := internal.NormalizeEmploymentType(field.Value); ok {
			posting.EmploymentType = employment
		}
	}

	if field, ok := icimsFirstField(fields, icimsPostedLabels); ok {
		posting.PostedAt = icimsTime(field.Title, order)
	}

	return posting, true
}

// icimsCards returns the job cards on one search page.
func icimsCards(doc *html.Node) []*html.Node {
	return icimsFindAll(doc, func(n *html.Node) bool {
		return n.Data == "li" && icimsHasClass(n, "iCIMS_JobCardItem")
	})
}

// icimsPageOrder reads the date order off a whole page before any posting on it
// is yielded.
//
// It has to be a separate pass. The order is a property of the board and the
// cards that settle it are not necessarily the first ones: on a page where every
// visible date is 7/3/2026 nothing is published for any card, which is the
// correct outcome, but one card reading 23/07/2026 settles the page.
func icimsPageOrder(cards []*html.Node) icimsDateOrder {
	var evidence icimsDateEvidence

	for _, card := range cards {
		if field, ok := icimsFirstField(icimsCardFields(card), icimsPostedLabels); ok {
			evidence.observe(field.Title)
		}
	}

	return evidence.order()
}

// ICIMS returns all of the job postings for one iCIMS classic career portal, or
// an error if there was a problem making the request or parsing the response.
//
// host is the tenant's full public hostname, see [ICIMSHosts].
//
// # Pagination
//
// The board states its own next page in <link rel="next"> and omits it on the
// last one, which is what ends the walk; see [icimsNextPage]. Three separate
// things bound it anyway, because this project has been bitten by an HTML
// pagination loop that trusted a board: the next page number must strictly
// increase, [pageRepeatGuard] ends the walk when a page repeats the previous
// page's posting ids, and [icimsMaxPages] holds unconditionally.
//
// # Duplicates within one board
//
// Measured on 2026-07-28: jobs-noodles returned 921 cards across 19 pages
// holding 893 distinct posting URLs. The board reorders between requests, so a
// posting can land on two pages of one walk. [internal.Dedupe] keys on URL and
// collapses them downstream, but a per-source count would still be inflated and
// [pageRepeatGuard] does not catch it -- the pages differ, they merely overlap.
// So this walk keeps its own set of yielded URLs. It is the one per-posting
// allocation here, bounded by the size of a single board.
func ICIMS(ctx context.Context, httpClient *http.Client, host string) internal.Jobs {
	// https://$host/jobs/search?pr=0&in_iframe=1
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			guard    pageRepeatGuard
			seen     = make(map[string]bool)
			page     = 0
			requests = 0
			cards    = 0
			yielded  = 0
		)

		for requests < icimsMaxPages {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			pageURL := icimsSearchURL(host, page)

			doc, err := fetchHTML(ctx, httpClient, icimsPlatform, host, pageURL)
			if err != nil {
				yield(nil, err)

				return
			}

			requests++

			pageCards := icimsCards(doc)
			cards += len(pageCards)

			order := icimsPageOrder(pageCards)

			ids := make([]string, 0, len(pageCards))

			for _, card := range pageCards {
				posting, ok := icimsCard(host, card, order)
				if !ok {
					continue
				}

				ids = append(ids, posting.URL)

				if seen[posting.URL] {
					continue
				}

				seen[posting.URL] = true
				yielded++

				if !yield(posting, nil) {
					return
				}
			}

			// Checked after the postings are yielded rather than before, so a
			// board that repeats a page still contributes that page's postings
			// once. The repeat is what ends the walk, not what discards it.
			if guard.repeated(ids) {
				break
			}

			next, ok := icimsNextPage(doc, page)
			if !ok {
				break
			}

			page = next
		}

		// A page full of cards that produced no posting at all means every
		// anchor on it failed the same-host check or carried no title, which no
		// live portal does. It is the signature of a template change or of a
		// host that serves someone else's cards, and reporting zero postings for
		// it would be indistinguishable from an employer that is not hiring.
		if cards > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from %s for company %q at %s: %d job cards parsed but none carried a posting URL on %s",
				icimsPlatform, host, icimsSearchURL(host, 0), cards, host))
		}
	}
}
