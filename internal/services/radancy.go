package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"golang.org/x/net/html"
)

// radancyPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
//
// Radancy is the current name of the product issue #18 knew as "TalentBrew",
// which is still the name on the wire: every tenant here loads
// tbcdn.talentbrew.com or cdn.radancy.eu. The platform is named for the vendor
// rather than the product because the product has already been renamed once.
const radancyPlatform = "radancy"

func init() {
	registerBuiltin(radancyPlatform, multiJobsFuncNamed(Radancy, RadancyTenants, radancyCompanyName))

	// Every tenant here keeps its own brand domain and points it at Radancy, so
	// unlike every other shared backend in httpx there is no suffix to match and
	// the hostnames have nothing textually in common. They are one backend all
	// the same: measured 2026-07-28, www.att.jobs and jobs.veolia.com both
	// resolve to 23.215.11.242 and careers.munichre.com to 23.215.11.240.
	//
	// Left to the generic exact-host policy each brand would get a private
	// four-slot limiter, so this platform alone would put up to 4*len(tenants)
	// concurrent requests on one backend -- the shape that rate-limited 56
	// Workable boards into looking dead. Registering the list from here rather
	// than copying it into httpx keeps one list, for the reason internal/shard
	// derives affinity from this same table instead of a second one.
	hosts := make([]string, 0, len(RadancyTenants))

	for _, key := range RadancyTenants {
		tenant, err := parseRadancyTenant(key)
		if err != nil {
			continue
		}

		hosts = append(hosts, tenant.host)
	}

	httpx.RegisterSharedBackend(radancyPlatform, hosts...)
}

const (
	// radancyPageSize is how many postings are asked for per request.
	//
	// Not a page size the platform imposes: RecordsPerPage is honoured verbatim
	// up to the result window below. Measured against jobs.walgreens.com, the
	// largest board found, one request returned exactly what was asked for at
	// every size tried — 100 rows in 68 KB and 1.3s, 1,000 in 673 KB and 2.3s,
	// 5,000 in 3.4 MB and 6.8s.
	//
	// 1,000 is chosen where response size stops being free. Six of the sixteen
	// registered tenants publish fewer than 1,000 postings, so they cost exactly
	// one request each, and the whole platform is 61 requests for ~48,900
	// postings — about 800 postings per request, against the 144 that made
	// SuccessFactors the cheapest lane in
	// docs/measurements/2026-07-28-crawl.md. Asking for 5,000 would save perhaps
	// thirty requests across the registry and in exchange hold several megabytes
	// of HTML, and then a parsed node tree several times that, in memory per
	// in-flight source.
	radancyPageSize = 1000

	// radancyMaxWindow is the deepest row this search will serve, measured.
	//
	// Bisected against jobs.walgreens.com, which publishes 21,232 postings:
	// CurrentPage=10&RecordsPerPage=1000 (offset 9,000) serves 1,000 rows and
	// CurrentPage=11 (offset 10,000) serves none; CurrentPage=2&RecordsPerPage=5000
	// serves 5,000 and CurrentPage=3 (offset 10,000) serves none;
	// CurrentPage=101&RecordsPerPage=100 (offset 10,000) serves none. A single
	// request for RecordsPerPage=25000 came back with exactly 10,000. The
	// server-rendered /search-jobs?p=N route hits the same wall in the same
	// place (p=600 serves 15 rows, p=700 serves none), so this is a property of
	// the platform's search index rather than of the endpoint or the page size.
	//
	// The consequence is that a board larger than 10,000 is reachable only down
	// to its 10,000th posting. That is the same shape, and the same number, as
	// [oracleCloudMaxWindow], and it is handled the same way: yielding 10,000
	// real postings is the correct outcome, and reporting it as an error would
	// mark a working board permanently failing.
	//
	// Walgreens is the one registered tenant this truncates: 21,232 published,
	// 10,000 reachable.
	radancyMaxWindow = 10000

	// radancyMaxPages is the number of page requests any tenant may make, and it
	// is unconditional: reaching it is the ordinary way a board larger than
	// [radancyMaxWindow] ends.
	//
	// It is derived from the window rather than chosen, so the two cannot drift.
	// The point of naming it is that it holds however the board behaves — a
	// tenant that ignores CurrentPage, or serves fewer rows than it was asked
	// for, or never runs out, still stops here. This project has twice paid for
	// an HTML pagination loop without that guarantee: eight adapters once
	// stopped only on a short page, and a board ignoring its page parameter
	// produced 500,001 duplicate postings in under a second. [pageRepeatGuard]
	// ends the ignored-parameter case one request sooner; this is what makes the
	// bound unconditional.
	radancyMaxPages = (radancyMaxWindow + radancyPageSize - 1) / radancyPageSize

	// radancySearchModule is the value Radancy's own front end sends for
	// SearchResultsModuleName, and it is mandatory.
	//
	// Measured on www.att.jobs: a request carrying CurrentPage and
	// RecordsPerPage but not this parameter answers HTTP 200 with
	// {"hasJobs":true} and an EMPTY results string — a live board that looks
	// like it published nothing. Adding this one parameter to the same URL
	// returned all 2,171 postings. It is the difference between this adapter
	// working and silently reporting every tenant as empty.
	radancySearchModule = "Search Results"
)

// RadancyTenants holds the Radancy (TalentBrew) career sites this project
// crawls, one "company,host[,pathPrefix]" entry per line.
//
// # Why the key is not just a hostname
//
// Two of the three parts cannot be derived from the other. The host is
// employer-owned and follows no convention (www.att.jobs, careers.aldi.us,
// jobs.disneycareers.com, emplois.disneycareers.com), so like Phenom it is a
// full hostname rather than a slug. The path prefix is a per-tenant locale
// segment that is part of the endpoint: jobs.veolia.com/search-jobs/results
// answers HTTP 301, and jobs.veolia.com/fr/search-jobs/results answers with the
// board. Five of the fourteen tenants below need one. And the company name is
// not recoverable from the host on jobs.disneycareers.com or
// jobs.tenethealth.com, so it is stated rather than guessed, the same decision
// [OracleCloudTenants] made for the same reason.
//
// Stating the name rather than deriving it also keeps
// TestNoUnreviewedDoubleCountedEmployer able to do its job. Deriving
// "tenethealth" from jobs.tenethealth.com would have registered Tenet Healthcare
// under a name that does not collide with the "tenet" already on Oracle Cloud,
// and the overlap check would have passed by not noticing — which, measured,
// would have hidden 1,437 shared job titles.
//
// # How this list was chosen
//
// Every entry was probed live on 2026-07-28 and is recorded with the posting
// count that probe returned. The counts are this project's own measurement, not
// an upstream annotation.
//
// One host that answered with a live board is deliberately absent.
// jobs.tenethealth.com publishes 3,350 postings and Tenet Healthcare is already
// registered on Oracle Cloud with 2,856; comparing them found 1,818 shared
// title+location pairs, 85% of the Oracle board, at the same facilities in the
// same proportions (San Antonio 342 against 345, West Palm Beach 233 against
// 237). Registering it would have bought roughly 500 postings that exist nowhere
// else at the price of counting 1,818 openings twice. Swapping the routes
// instead is a reasonable call and a deliberate one, so the measurement is
// staged rather than acted on unilaterally.
//
// That measurement, the hosts that are the same board under another locale, the
// ones behind a bot wall, and the ones that turned out not to be Radancy at all
// are all staged with their evidence at
// testdata/candidates/radancy_hosts.txt.
var RadancyTenants = []string{
	"walgreens,jobs.walgreens.com,/en",                // 21,232, truncated to 10,000 by radancyMaxWindow
	"chipotle,jobs.chipotle.com",                      // 7,659
	"unitedhealthgroup,careers.unitedhealthgroup.com", // 5,910
	"vinci,jobs.vinci.com,/en",                        // 5,878
	"citi,jobs.citi.com",                              // 3,484
	"veolia,jobs.veolia.com,/fr",                      // 2,962
	"att,www.att.jobs",                                // 2,171
	"munichre,careers.munichre.com,/en",               // 1,517
	"geisinger,jobs.geisinger.org",                    // 1,421
	"aldi,careers.aldi.us",                            // 1,213
	"sanofi,jobs.sanofi.com,/en",                      // 1,015
	"takeda,jobs.takeda.com",                          // 886
	"disney,jobs.disneycareers.com",                   // 800
	"wegmans,jobs.wegmans.com,/en",                    // 499
	"parexel,jobs.parexel.com,/en",                    // 227
	"carnival,jobs.carnival.com",                      // 128
}

// radancyTenant is one entry of [RadancyTenants], parsed.
type radancyTenant struct {
	company string
	host    string
	prefix  string
}

// parseRadancyTenant splits a "company,host[,pathPrefix]" key.
func parseRadancyTenant(key string) (radancyTenant, error) {
	fields := strings.Split(key, ",")
	if len(fields) < 2 || len(fields) > 3 {
		return radancyTenant{}, fmt.Errorf("malformed Radancy tenant %q: want \"company,host\" or \"company,host,/pathPrefix\"", key)
	}

	tenant := radancyTenant{
		company: strings.TrimSpace(fields[0]),
		host:    strings.TrimSpace(fields[1]),
	}

	if len(fields) == 3 {
		// Stored with a leading slash and no trailing one, but normalised here
		// so a hand-edited entry cannot produce a double slash in the URL.
		tenant.prefix = "/" + strings.Trim(strings.TrimSpace(fields[2]), "/")
		if tenant.prefix == "/" {
			tenant.prefix = ""
		}
	}

	if tenant.company == "" || tenant.host == "" {
		return radancyTenant{}, fmt.Errorf("malformed Radancy tenant %q: company and host are both required", key)
	}

	return tenant, nil
}

// radancyCompanyName returns the display name stated in a tenant key.
func radancyCompanyName(key string) string {
	tenant, err := parseRadancyTenant(key)
	if err != nil {
		return key
	}

	return tenant.company
}

// radancyResults is the envelope the search endpoint returns.
//
// Only Results is read. The response also carries "filters" (a megabyte of
// facet markup on the larger tenants), "hasJobs" and "hasContent"; none of them
// says anything the parsed results do not, and "filters" in particular is
// deliberately left unmodelled so it is discarded rather than retained.
type radancyResults struct {
	Results string `json:"results"`
}

// radancyPage fetches one page of a tenant's search results.
//
// Split out so the response body is closed per page rather than accumulating one
// open body per page for the lifetime of a crawl.
func radancyPage(ctx context.Context, httpClient *http.Client, tenant radancyTenant, page int) (*html.Node, error) {
	query := url.Values{
		"CurrentPage":             {strconv.Itoa(page)},
		"RecordsPerPage":          {strconv.Itoa(radancyPageSize)},
		"SearchResultsModuleName": {radancySearchModule},
	}

	endpoint := fmt.Sprintf("https://%s%s/search-jobs/results?%s", tenant.host, tenant.prefix, query.Encode())

	envelope, err := fetchJSON[radancyResults](ctx, httpClient, radancyPlatform, tenant.company, jsonRequest{URL: endpoint})
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(strings.NewReader(envelope.Results))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML from %s for company %q: %w", radancyPlatform, tenant.company, err)
	}

	return doc, nil
}

// radancyTotalAttrs are the attributes the results fragment states its size in,
// most specific first.
//
// Both are present on every tenant measured, and on Wegmans they disagree:
// data-total-results is 605 while data-total-job-results is 499, the difference
// being the 106 non-job content pages that same search also matched. The job
// count is the one this adapter is about.
var radancyTotalAttrs = []string{"data-total-job-results", "data-total-results"}

// radancyTotal reports the posting count the results fragment states.
//
// The second return distinguishes "this tenant published nothing" from "this
// host is not a Radancy board", which nothing else in the response does. A real
// Radancy search that matches no posting still renders its results section with
// the count attribute set to 0 (measured on www.att.jobs with a nonsense
// keyword), so an absent attribute means the response was not a Radancy results
// fragment at all.
func radancyTotal(doc *html.Node) (int, bool) {
	var (
		total int
		found bool
		walk  func(*html.Node)
	)

	walk = func(n *html.Node) {
		if found || n == nil {
			return
		}

		if n.Type == html.ElementNode {
			for _, want := range radancyTotalAttrs {
				for _, attr := range n.Attr {
					if attr.Key != want {
						continue
					}

					if value, err := strconv.Atoi(strings.TrimSpace(attr.Val)); err == nil {
						total, found = value, true
						return
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)

	return total, found
}

// radancyAttr returns an element's attribute value.
func radancyAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}

	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}

	return ""
}

// radancyJobLinks returns the posting anchors on a results fragment, in document
// order and deduplicated by job id.
//
// # The invariant
//
// An <a> carrying BOTH an href and a data-job-id is the one thing every skin
// measured has in common, and both halves of that are load-bearing. The "Save
// Job" control on nine of the fourteen tenants is a <button> with the same
// data-job-id and no href, so requiring the href is what keeps a posting from
// being counted twice. Requiring the data-job-id is what keeps the surrounding
// navigation out.
//
// # What is deliberately NOT part of the invariant
//
// docs/research/ats-platform-survey.md records the invariant as an <a> with
// data-job-id and `href="/job/..."`. The href prefix does not hold: paths are
// locale-prefixed on six tenants (/en/job/..., /fr/emploi/...) and the path
// segment itself is translated on jobs.veolia.com, where postings live under
// /fr/emploi/ and matching on "/job/" finds none of the 2,962. So the href is
// required to exist and is not otherwise inspected.
//
// Deduplication is not belt-and-braces either: jobs.veolia.com renders two
// anchors per posting, a title link and a chevron link, both carrying the same
// href and data-job-id, so a run over that tenant without this returns exactly
// twice the board.
func radancyJobLinks(doc *html.Node) []*html.Node {
	var (
		links []*html.Node
		seen  = map[string]bool{}
		walk  func(*html.Node)
	)

	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			id := radancyAttr(n, "data-job-id")

			if id != "" && radancyAttr(n, "href") != "" && !seen[id] {
				seen[id] = true
				links = append(links, n)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)

	return links
}

// radancyRow returns the element that holds everything about one posting.
//
// The anchor is not enough on its own: on www.att.jobs the location is a sibling
// of the anchor's grandparent, and on jobs.disneycareers.com the posting is a
// table row whose title, date, brand and location are four separate <td>s. Both
// resolve by walking up to the nearest list item or table row, which is the row
// container on every skin measured. An anchor with neither ancestor falls back
// to itself, which still yields a title and a URL.
func radancyRow(anchor *html.Node) *html.Node {
	for n := anchor.Parent; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && (n.Data == "li" || n.Data == "tr") {
			return n
		}
	}

	return anchor
}

// radancyClassTokens splits an element's class attribute.
func radancyClassTokens(n *html.Node) []string {
	return strings.Fields(radancyAttr(n, "class"))
}

// radancyFindByClass returns the first element under root whose class list
// contains a token satisfying match.
func radancyFindByClass(root *html.Node, match func(token string) bool) *html.Node {
	var (
		found *html.Node
		walk  func(*html.Node)
	)

	walk = func(n *html.Node) {
		if found != nil || n == nil {
			return
		}

		if n.Type == html.ElementNode {
			for _, token := range radancyClassTokens(n) {
				if match(token) {
					found = n
					return
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(root)

	return found
}

// radancyFirstByClassSubstring returns the text of the first element under root
// whose class contains any of the given substrings, trying each substring across
// the whole subtree before moving to the next.
//
// The ordering matters because skins disagree about which field is which.
// jobs.chipotle.com carries both a "job-location" ("Grand Island, NE") and a
// "job-address" ("3440 W. State St., 68803"); careers.aldi.us carries only
// "job-address-list" and it holds the full street address. Searching for
// job-location everywhere first is what makes Chipotle produce a city and Aldi
// still produce something.
func radancyFirstByClassSubstring(root *html.Node, substrings ...string) string {
	for _, want := range substrings {
		node := radancyFindByClass(root, func(token string) bool {
			return strings.Contains(token, want)
		})

		if node == nil {
			continue
		}

		if text := radancyValueText(node); text != "" {
			return text
		}
	}

	return ""
}

// radancyLabelElements are the tags and class tokens a skin uses to print a
// field's caption inside the field's own element.
//
// Three of the tenants measured do this and each picked a different wrapper:
// jobs.tenethealth.com writes <span class="job-type"><span class="heading">Job
// Type: </span>PRN</span>, careers.aldi.us uses <b>Position Type</b> and
// jobs.takeda.com uses <strong>Category: </strong>. Reading the element's whole
// text would file the department of a Takeda role as "Category: Medical
// Affairs".
var radancyLabelElements = map[string]bool{"b": true, "strong": true, "label": true}

// radancyValueText returns an element's text with its caption removed, collapsed
// to single spaces and trimmed of the separators skins put between a caption and
// its value.
func radancyValueText(n *html.Node) string {
	if n == nil {
		return ""
	}

	var (
		out  strings.Builder
		walk func(*html.Node)
	)

	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
			return
		}

		if n.Type == html.ElementNode {
			if radancyLabelElements[n.Data] {
				return
			}

			for _, token := range radancyClassTokens(n) {
				// "heading" is Tenet's caption class. "sr-only", "wai" and
				// "visually-hidden" are screen-reader duplicates of a value that
				// is already in the row: jobs.vinci.com repeats every location
				// and category in an sr-only span, so including them doubles the
				// text.
				switch token {
				case "heading", "sr-only", "wai", "visually-hidden", "always-hidden":
					return
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(n)

	return strings.Trim(strings.Join(strings.Fields(out.String()), " "), " |:-–—")
}

// radancyTitle returns a posting's title.
//
// A heading first, because eleven of the fourteen skins put one in the row,
// either wrapping the anchor (www.att.jobs, jobs.citi.com) or inside it
// (everyone else). jobs.vinci.com has no heading at all and names the title with
// a class instead, and the anchor's own text is the last resort — which is
// correct for the wrapping-heading skins and wrong for Vinci, whose anchor text
// also contains the location, category and job type, hence the class check in
// between.
func radancyTitle(row, anchor *html.Node) string {
	headings := map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true}

	var (
		found *html.Node
		walk  func(*html.Node)
	)

	walk = func(n *html.Node) {
		if found != nil || n == nil {
			return
		}

		if n.Type == html.ElementNode && headings[n.Data] {
			found = n
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(row)

	if text := radancyValueText(found); text != "" {
		return text
	}

	if text := radancyFirstByClassSubstring(row, "jobtitle", "job-title"); text != "" {
		return text
	}

	return radancyValueText(anchor)
}

// radancyDateLayouts are the spellings the "job-date-posted" field uses.
//
// Both were measured, and unlike Phenom's the slash form is safe to read here.
// Across 2,016 dated postings from careers.munichre.com and jobs.wegmans.com the
// second component reached 31 and the first never exceeded 12, which rules out
// day-first: no month is the 31st. jobs.disneycareers.com writes the month as a
// name instead, abbreviated with a trailing period except for May, which is
// already three letters — so both "Jul. 27, 2026" and "May 18, 2026" occur and
// both layouts are needed.
var radancyDateLayouts = []string{
	"01/02/2006",
	"Jan. 2, 2006",
	"Jan 2, 2006",
}

// radancyPostedAt parses a posted date, reporting false when it is missing or in
// a spelling this does not know.
func radancyPostedAt(text string) (time.Time, bool) {
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range radancyDateLayouts {
		if posted, err := time.Parse(layout, text); err == nil {
			return posted.UTC(), true
		}
	}

	return time.Time{}, false
}

// radancyPosting converts one row of a results fragment into a posting,
// reporting false when it carries no title or no resolvable URL.
func radancyPosting(tenant radancyTenant, base *url.URL, anchor *html.Node) (*internal.JobPosting, bool) {
	row := radancyRow(anchor)

	title := radancyTitle(row, anchor)
	if title == "" {
		return nil, false
	}

	href, err := url.Parse(radancyAttr(anchor, "href"))
	if err != nil {
		return nil, false
	}

	// Resolved rather than concatenated: hrefs are root-relative on every skin
	// measured, but a tenant that ever emits an absolute one would otherwise
	// produce "https://host/https://host/job/...".
	link := base.ResolveReference(href).String()

	location := radancyFirstByClassSubstring(row, "job-location", "job-address", "location")
	if location == "" {
		location = "unknown/remote"
	}

	posting := &internal.JobPosting{
		Company:  tenant.company,
		URL:      link,
		Title:    title,
		Location: location,

		Department: radancyFirstByClassSubstring(row, "job-categor", "job-department", "category"),
		ExternalID: radancyAttr(anchor, "data-job-id"),
		Source:     internal.PostingSource{Platform: radancyPlatform, Key: tenantKey(tenant)},
	}

	if employment, ok := internal.NormalizeEmploymentType(radancyFirstByClassSubstring(row, "job-type", "jobtype")); ok {
		posting.EmploymentType = employment
	}

	if posted, ok := radancyPostedAt(radancyFirstByClassSubstring(row, "job-date-posted", "date-posted")); ok {
		posting.PostedAt = posted
	}

	return posting, true
}

// tenantKey rebuilds the registry key for a parsed tenant, so
// [internal.PostingSource.Key] matches [Source.Key] exactly.
func tenantKey(tenant radancyTenant) string {
	if tenant.prefix == "" {
		return tenant.company + "," + tenant.host
	}

	return tenant.company + "," + tenant.host + "," + tenant.prefix
}

// Radancy returns the job postings for a company hosted on a Radancy
// (TalentBrew) career site.
//
// company is a "company,host[,pathPrefix]" key, see [RadancyTenants].
//
// # Why this uses the AJAX endpoint and not /search-jobs?p=N
//
// docs/research/ats-platform-survey.md ranks this platform among the ones that
// "will blow the time budget", on the grounds that it is HTML at ~15 rows per
// page and that "the AJAX /search-jobs/results endpoint IGNORES every filter
// query param and returns the full global list". Measured on 2026-07-28, both
// halves of that are wrong in the same direction.
//
// The endpoint honours RecordsPerPage, CurrentPage and Keywords: a request for
// RecordsPerPage=5000 against www.att.jobs returned all 2,171 postings in one
// response, CurrentPage=2 returned a disjoint page, and Keywords=engineer
// narrowed to 263 while a nonsense keyword returned none. What it will not do
// without SearchResultsModuleName is return anything at all, which is the far
// likelier explanation for a note saying its parameters do nothing.
//
// The server-rendered route really is 12 to 21 rows per page, so Walgreens is
// 1,416 requests that way and 10 this way. Across the registered tenants this is
// roughly 1,700 postings per HTTP request, which makes Radancy the cheapest lane
// in the registry rather than one of the most expensive — see
// docs/measurements/2026-07-28-crawl.md for what it is being compared against.
func Radancy(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		tenant, err := parseRadancyTenant(company)
		if err != nil {
			yield(nil, err)
			return
		}

		base := &url.URL{Scheme: "https", Host: tenant.host}

		var (
			pages   pageRepeatGuard
			yielded int
		)

		for pageCount := 1; pageCount <= radancyMaxPages; pageCount++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			doc, err := radancyPage(ctx, httpClient, tenant, pageCount)
			if err != nil {
				yield(nil, err)
				return
			}

			total, stated := radancyTotal(doc)
			if !stated {
				// Not a Radancy results fragment. Reported rather than treated
				// as an empty board, because the two are otherwise identical
				// from here and a misfiled host would sit in the registry
				// forever returning nothing.
				yield(nil, fmt.Errorf("response from %s for company %q carries no result count: %q may not be a Radancy career site",
					radancyPlatform, tenant.company, tenant.host))

				return
			}

			links := radancyJobLinks(doc)
			if len(links) == 0 {
				return
			}

			ids := make([]string, 0, len(links))
			for _, link := range links {
				ids = append(ids, radancyAttr(link, "data-job-id"))
			}

			// Checked before anything is yielded, so a tenant whose search
			// ignores CurrentPage costs one wasted request rather than an
			// endless stream of duplicates.
			if pages.repeated(ids) {
				return
			}

			for _, link := range links {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				posting, ok := radancyPosting(tenant, base, link)
				if !ok {
					continue
				}

				if !yield(posting, nil) {
					return
				}

				yielded++
			}

			// The board states its own size, so a tenant smaller than one page
			// ends here without spending a request to discover it is finished.
			if len(links) < radancyPageSize || yielded >= total {
				return
			}
		}

		// Reaching here means the board was still serving full pages when the
		// page bound ran out, which for this platform means the search's result
		// window closed on a board bigger than 10,000.
		//
		// Deliberately not an error. Walgreens publishes 21,232 postings and
		// exactly 10,000 of them are reachable, so this path runs on every crawl
		// of a working, correctly configured tenant; erroring here reported that
		// source as failing every night while it returned 10,000 real postings.
		// That was measured, not hypothesised — it is what the first version of
		// this adapter did.
	}
}
