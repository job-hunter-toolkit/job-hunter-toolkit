package services

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

// eightfoldPlatform is the platform name this file registers under, shared with
// the [internal.PostingSource] every posting carries so the two cannot drift
// apart.
const eightfoldPlatform = "eightfold"

func init() {
	registerBuiltin(eightfoldPlatform, multiJobsFunc(Eightfold, EightfoldCompanies))
}

// eightfoldPageSize is the number of postings requested per page.
//
// The server hard-caps a page at ten regardless of what "num" asks for: num=20,
// num=25 and num=50 all answer with exactly ten positions. Sending the real
// ceiling keeps the URL honest about what comes back, and makes a page shorter
// than this a trustworthy end-of-list signal.
const eightfoldPageSize = 10

// eightfoldMaxPages bounds how many pages a single Eightfold tenant may be asked
// for.
//
// The normal stop is a short or empty page, and [pageRepeatGuard] catches a
// tenant that ignores "start" entirely. This is the backstop for the case
// neither of those can see: a tenant that keeps serving *different* full pages
// forever. At ten postings per page it allows 20,000 postings from one company,
// an order of magnitude above the largest tenant registered here (HSBC, ~1,500),
// so reaching it means something is wrong — and because what was yielded is then
// a truncated board, [Eightfold] reports an error rather than returning quietly.
const eightfoldMaxPages = 2000

// EightfoldCompanies holds the Eightfold tenant slugs this project crawls.
//
// Each entry is the subdomain label of a {slug}.eightfold.ai careers site. Many
// tenants also answer on a branded host — talent.bayer.com, careers.fluor.com,
// explore.jobs.netflix.net — and that branded host is what
// canonicalPositionUrl, and therefore the posting URL, points at. The
// eightfold.ai label is used as the key anyway because it is uniform across
// tenants and is what the API is addressed by; the branded hosts are not
// derivable from it.
//
// **Every slug here answered one of the two crawl routes with postings on two
// separate runs.** That matters more than usual on this platform: roughly three
// quarters of Eightfold tenants answer the list API with HTTP 403 and
// `{"message": "Not authorized for PCSX"}` instead, and a walled tenant is
// indistinguishable from a live one by name alone. See
// docs/source-backlog.md for the walled list and what is known about it.
//
// Since 2026-07-30 a gated tenant is no longer automatically uncrawlable: when
// the list API answers the PCSX wall, [Eightfold] falls back to the tenant's
// public sitemap plus the schema.org JSON-LD on each job page. The tenants
// registered on that route are the ones whose sitemap was measured real (54 of
// the 109 gated tenants publish one; 3 publish a decoy pointing at Eightfold's
// own board and 52 publish none) AND whose job pages actually carry JSON-LD
// (trimble's do not) AND whose board is small enough that one-request-per-
// posting is affordable (500 postings or fewer measured; the 15 larger real
// sitemaps, from ericsson's 501 to starbucks' 21,834, stay staged in the
// candidate file with their sizes). The 22 that passed all three gates are
// marked "sitemap route" below; together they measured 4,051 postings on
// 2026-07-30, which is what that route costs per crawl in requests.
//
// A handful of slugs are the employer's ticker or a product name rather than
// something a job seeker would type, which docs/adding-a-source.md warns about;
// no longer, unambiguous slug exists for them, so they are named here instead:
//
//   - "ftr" is Frontier Communications (careers.frontier.com)
//   - "gotinder" is Match Group as a whole (join.matchgroupcareers.com), not
//     Tinder alone
//   - "insight" is Insight Enterprises, the IT reseller
//   - "bcg" is Boston Consulting Group's student/campus board
//     (studenttalent.bcg.com), not its experienced-hire board
//
// Bayer, Freeport-McMoRan (fcx) and NetApp are deliberately absent even though
// their Eightfold boards answer: all three are already registered on SAP
// SuccessFactors, and [internal.Dedupe] keys on URL, so the same job published
// to both platforms has two URLs and would be counted twice in jobs_record.txt.
// SuccessFactors is the better of the two routes for them on every axis that
// matters — it returns a whole board in one request against roughly 64 here for
// Bayer, and its response carries the job description this list response leaves
// empty, which is what pay parsing reads. The coverage given up is small:
// measured on the same day, SuccessFactors served 286 against Eightfold's 289
// for fcx and 281 against 272 for netapp. Bayer is the one real trade, 482
// against 638. TestSuccessFactorsAddsNoDoubleCountedEmployer enforces the rule.
var EightfoldCompanies = []string{
	"10xgenomics", // sitemap route, 25 measured 2026-07-30
	"albemarle",
	"alnylam", // sitemap route, 152
	"amdocs",  // sitemap route, 7
	"atb",     // sitemap route, 45; ATB Financial
	"bcg",
	"bluecrabconsulting", // sitemap route, 2
	"britishcouncil",     // sitemap route, 82
	"ccep",               // sitemap route, 45; Coca-Cola Europacific Partners
	"coca-colafemsa",
	"costar",
	"dexcom", // sitemap route, 294
	"dsm",    // sitemap route, 425; dsm-firmenich
	"faurecia",
	"fluor",
	"foleyeq", // sitemap route, 78
	"ftr",
	"globalfoundries", // sitemap route, 500
	"gotinder",
	"houstonisd",
	"hsbc",
	"insight",
	"johndeere", // sitemap route, 204
	"libertymutual",
	"mtsi-va", // sitemap route, 390; Modern Technology Solutions Inc
	"netflix",
	"oxxo",
	"paypal",   // sitemap route, 152
	"ralliant", // sitemap route, 240
	"rotoplas", // sitemap route, 15
	"softtek",  // sitemap route, 334
	"stmicroelectronics",
	"symetra",
	"telekom-growthhub", // sitemap route, 223; Deutsche Telekom's internal-mobility brand
	"tevapharm",
	"trinet", // sitemap route, 205
	"ukg",    // sitemap route, 288
	"vale",
	"vialto",     // sitemap route, 140
	"vizientinc", // sitemap route, 205
}

// eightfoldJobs is the subset of an Eightfold list response this adapter uses.
//
// The response also carries the tenant's whole careers-site configuration —
// branding, facets, geolocation, an empty candidate record — which together
// dwarf the postings; none of it is modelled. What is deliberately absent from
// the model is job_description: the field exists on every position but is empty
// in the list response for every tenant checked, so there is no prose here for
// [internal.CompensationFromText] to read and no pay to publish. Filling it
// would cost one request per posting against the per-job endpoint, which is the
// trade docs/research/ats-platform-survey.md rejects for this platform.
//
// The tenant's total ("count") is deliberately not modelled either, even though
// it is free in this same response: see the walk in [Eightfold] for why it is
// not allowed to end the paging.
type eightfoldJobs struct {
	Positions []struct {
		// ID is the Eightfold position id, the number in canonicalPositionUrl.
		ID int64 `json:"id"`

		Name     string `json:"name"`
		Location string `json:"location"`

		// Department and BusinessUnit are both org-unit labels, and which of the
		// two is the coarser one is not consistent across tenants: Bayer files
		// "Medical Affairs & Pharmacovigilance" (department) under
		// "Pharmaceuticals" (business_unit), while HSBC files "Fin Sustain & Grp
		// Ext Comm" (business_unit) under "Finance" (department). They are mapped
		// by name rather than by guessing granularity per tenant, and
		// [internal.Filter.Departments] searches both fields, so the ordering
		// costs a job seeker nothing.
		//
		// Both are [eightfoldText] because Eightfold does not hold their type
		// stable. Fluor — 716 postings, the second-largest tenant registered
		// here — sends department as a JSON array (["Operations & Maintenance"]),
		// while every other tenant sends a bare string and some send null.
		// Modelling it as a Go string would fail the decode for the whole page,
		// and fetchJSON decodes a page at once, so that one field would take down
		// every Fluor posting.
		Department   eightfoldText `json:"department"`
		BusinessUnit eightfoldText `json:"business_unit"`

		// TCreate and TUpdate are Unix seconds.
		TCreate int64 `json:"t_create"`
		TUpdate int64 `json:"t_update"`

		// DisplayJobID is the employer's own requisition number ("877989"), which
		// is what a referral form or a recruiter asks for. It is not the same
		// thing as ID.
		DisplayJobID eightfoldText `json:"display_job_id"`

		// WorkLocationOption is Eightfold's own remote/hybrid/onsite field:
		// "onsite", "hybrid", "remote_local", "remote_global".
		WorkLocationOption string `json:"work_location_option"`

		// CanonicalPositionURL is the posting's public URL, usually on the
		// employer's branded careers host rather than on eightfold.ai.
		CanonicalPositionURL string `json:"canonicalPositionUrl"`
	} `json:"positions"`
}

// eightfoldText decodes a JSON value whose type Eightfold does not hold stable
// into a string.
//
// Kept separate from the other tolerant scalars in this package even though
// several of them look alike today: each one describes what one third-party API
// actually does, and this one has to accept a case none of the others do, an
// array of strings. Anything unreadable becomes the empty string rather than an
// error, because one odd field must never cost a board its postings.
type eightfoldText string

// UnmarshalJSON implements [json.Unmarshaler]. It never reports an error.
func (t *eightfoldText) UnmarshalJSON(data []byte) error {
	*t = ""

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return nil
		}

		*t = eightfoldText(text)

	case '[':
		// Fluor's department. Take the first non-empty entry rather than joining:
		// the array holds one label for every posting seen, and a join would
		// invent a compound department name if that ever stops being true.
		var items []string
		if err := json.Unmarshal(data, &items); err != nil {
			return nil
		}

		for _, item := range items {
			if item = strings.TrimSpace(item); item != "" {
				*t = eightfoldText(item)

				break
			}
		}

	case '{':
		// An object is not a label, and rendering its literal JSON would publish
		// "{...}" as an employer's department.

	default:
		*t = eightfoldText(trimmed)
	}

	return nil
}

// String returns the decoded text with surrounding whitespace removed.
func (t eightfoldText) String() string {
	return strings.TrimSpace(string(t))
}

// eightfoldTimestamp converts one of Eightfold's Unix-second stamps to UTC,
// returning the zero time when the field was absent or not a plausible date.
//
// A non-positive value is the common absence, and the upper guard rejects a
// value that is really milliseconds: t_create and t_update are seconds on every
// tenant checked, but a tenant that switched units would otherwise date every
// one of its postings to the year 58000 and quietly satisfy every
// [internal.Filter.PostedSince] query.
func eightfoldTimestamp(seconds int64) time.Time {
	const year2100 = 4102444800 // 2100-01-01T00:00:00Z

	if seconds <= 0 || seconds > year2100 {
		return time.Time{}
	}

	return time.Unix(seconds, 0).UTC()
}

// eightfoldListURL is the list API for one page of a tenant's postings.
func eightfoldListURL(company string, start int) string {
	return "https://" + company + ".eightfold.ai/api/apply/v2/jobs" +
		"?start=" + strconv.Itoa(start) +
		"&num=" + strconv.Itoa(eightfoldPageSize)
}

// eightfoldFirstPage fetches page 0 of the list API with status visibility,
// because HTTP 403 is a meaningful answer on this platform rather than a
// failure: it is the per-tenant PCSX wall docs/adding-a-source.md describes, and
// it is what routes a tenant onto the sitemap fallback.
//
// gated is reported only for a 403 whose body carries the wall's own "PCSX"
// marker. A bare 403 from a proxy or a WAF is not the wall, and treating it as
// one would quietly reroute a healthy list tenant onto a route that costs one
// request per posting.
func eightfoldFirstPage(ctx context.Context, httpClient *http.Client, company string) (doc *eightfoldJobs, gated bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eightfoldListURL(company, 0), nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request for Eightfold company %q: %w", company, err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to make request to Eightfold for company %q: %w", company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if strings.Contains(string(body), "PCSX") {
			return nil, true, nil
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status code from Eightfold for company %q: %s", company, resp.Status)
	}

	doc = new(eightfoldJobs)
	if err := json.NewDecoder(resp.Body).Decode(doc); err != nil {
		return nil, false, fmt.Errorf("failed to decode response from Eightfold for company %q: %w", company, err)
	}

	return doc, false, nil
}

// Eightfold returns all of the job postings for a given company, or an error if
// there was a problem making the request or parsing the response.
//
// The list API is the primary route: ten postings per request against roughly
// one per request on the fallback below, so it is tried first for every tenant.
//
// A tenant that answers HTTP 403 with `{"message": "Not authorized for PCSX"}`
// is not broken infrastructure — Eightfold gates the list API per tenant, 109
// of 133 live tenants measured — and since 2026-07-30 it is not the end of the
// road either: those tenants fall back to the public sitemap at
// /careers/sitemap.xml plus the schema.org JSON-LD block on each job page,
// which the wall does not cover. See [eightfoldSitemapWalk] for that route's
// own verification rules and costs.
func Eightfold(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			guard pageRepeatGuard
			start int
		)

		first, gated, err := eightfoldFirstPage(ctx, httpClient, company)
		if err != nil {
			yield(nil, err)

			return
		}

		if gated {
			eightfoldSitemapWalk(ctx, httpClient, company, yield)

			return
		}

		for page := 0; page < eightfoldMaxPages; page++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			doc := first
			if page > 0 {
				doc, err = fetchJSON[eightfoldJobs](ctx, httpClient, "Eightfold", company, jsonRequest{
					URL: eightfoldListURL(company, start),
				})
				if err != nil {
					yield(nil, err)

					return
				}
			}

			if len(doc.Positions) == 0 {
				return
			}

			ids := make([]string, 0, len(doc.Positions))
			for _, item := range doc.Positions {
				ids = append(ids, strconv.FormatInt(item.ID, 10))
			}

			if guard.repeated(ids) {
				return
			}

			for _, item := range doc.Positions {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())

					return
				}

				url := strings.TrimSpace(item.CanonicalPositionURL)
				if url == "" && item.ID > 0 {
					// Every tenant checked publishes canonicalPositionUrl, but a
					// posting with no URL is useless, and the eightfold.ai host
					// serves the same posting the branded host does.
					url = "https://" + company + ".eightfold.ai/careers/job/" +
						strconv.FormatInt(item.ID, 10)
				}

				location := strings.TrimSpace(item.Location)
				if location == "" {
					location = "unknown/remote"
				}

				workplace := eightfoldWorkplaceType(item.WorkLocationOption)

				posting := &internal.JobPosting{
					Company:       company,
					URL:           url,
					Title:         strings.TrimSpace(item.Name),
					Location:      location,
					Department:    item.Department.String(),
					Team:          item.BusinessUnit.String(),
					WorkplaceType: workplace,
					Remote:        eightfoldRemote(workplace),
					PostedAt:      eightfoldTimestamp(item.TCreate),
					UpdatedAt:     eightfoldTimestamp(item.TUpdate),
					RequisitionID: item.DisplayJobID.String(),
					Source: internal.PostingSource{
						Platform: eightfoldPlatform,
						Key:      company,
					},
				}

				if item.ID > 0 {
					posting.ExternalID = strconv.FormatInt(item.ID, 10)
				}

				if !yield(posting, nil) {
					return
				}
			}

			// A short page is the end of the list. The response also carries a
			// "count" total, and stopping on it would save the one empty request
			// a board whose posting count is an exact multiple of the page size
			// costs. It is deliberately not used for that: a count lower than
			// what the pages actually serve would silently truncate the board,
			// and losing postings is a far worse failure here than spending one
			// more request.
			if len(doc.Positions) < eightfoldPageSize {
				return
			}

			start += len(doc.Positions)
		}

		// Falling out of the loop means the ceiling was reached while the board
		// was still serving full pages, so what was yielded is a truncated board.
		// Returning silently here would let `health` call the source ok and hide
		// exactly the failure the ceiling exists to catch, so it is reported the
		// way Lever and Teamtailor report theirs.
		yield(nil, fmt.Errorf("refusing to keep paginating Eightfold for company %q: the board was still serving full pages after %d pages of %d",
			company, eightfoldMaxPages, eightfoldPageSize))
	}
}

const (
	// eightfoldSitemapPath is the tenant-relative path of the public sitemap the
	// fallback route reads. The PCSX wall on the list API does not cover it: 54
	// of the 109 gated tenants measured on 2026-07-30 serve a real one here.
	eightfoldSitemapPath = "/careers/sitemap.xml"

	// eightfoldSitemapMaxBytes bounds how much sitemap is read. The largest real
	// sitemap measured (starbucks) is 6.1 MB for 21,834 postings; 16 MB is well
	// past anything a registrable tenant publishes, and a sitemap bigger than
	// this fails [eightfoldSitemapMaxPostings] anyway.
	eightfoldSitemapMaxBytes = 16 << 20

	// eightfoldSitemapMaxPostings bounds how many job pages one gated tenant may
	// cost, and exceeding it is an error rather than a truncation.
	//
	// The sitemap route costs one request per posting — Eightfold's job pages
	// carry no list API for a gated tenant — so a board's size IS its request
	// bill, and registering a tenant on this route is a cost decision made from
	// a measured size. Every tenant registered on it measured 500 or fewer
	// postings on 2026-07-30 (4,051 across all 22); the ceiling is 4x the
	// largest so ordinary growth does not trip it. A tenant that outgrows it
	// wants that decision re-made, not silently paid: yielding a partial board
	// would violate the rule that a partial crawl never looks complete, so the
	// walk refuses up front.
	eightfoldSitemapMaxPostings = 2000
)

// eightfoldSitemap is the subset of a sitemap.xml document the fallback uses.
type eightfoldSitemap struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// eightfoldSitemapLocs fetches a gated tenant's sitemap and returns its job-page
// URLs exactly as published.
//
// Three shapes come back from the 109 gated tenants and only one of them is
// crawlable, so the other two are named errors rather than empty successes:
//
//   - 54 tenants publish a real sitemap whose <loc> entries point at the
//     tenant's own board (careers.deere.com, paypal.eightfold.ai, ...);
//   - 3 publish a DECOY: a byte-identical sitemap listing Eightfold's own
//     careers board at app.eightfold.ai?domain=eightfold.ai (target, kroger,
//     qa all serve the same 65 postings — Eightfold's, not theirs). Crawling
//     it would attribute Eightfold's openings to the tenant, the Oracle
//     benchmark-tenant mistake with a different vendor;
//   - 52 answer 404 or an empty document, so the tenant is genuinely
//     unreachable and the error says exactly that.
func eightfoldSitemapLocs(ctx context.Context, httpClient *http.Client, company string) ([]string, error) {
	sitemapURL := "https://" + company + ".eightfold.ai" + eightfoldSitemapPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Eightfold company %q: %w", company, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Eightfold for company %q: %w", company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Eightfold company %q gates its list API and its sitemap answered %s: not crawlable by either route", company, resp.Status)
	}

	var doc eightfoldSitemap
	if err := xml.NewDecoder(io.LimitReader(resp.Body, eightfoldSitemapMaxBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode sitemap from Eightfold for company %q: %w", company, err)
	}

	locs := make([]string, 0, len(doc.URLs))

	for _, entry := range doc.URLs {
		loc := strings.TrimSpace(entry.Loc)
		if !strings.Contains(loc, "/careers/job/") {
			continue
		}

		parsed, err := url.Parse(loc)
		if err != nil {
			continue
		}

		if strings.EqualFold(parsed.Hostname(), "app.eightfold.ai") &&
			parsed.Query().Get("domain") == "eightfold.ai" && company != "eightfold" {
			return nil, fmt.Errorf("Eightfold company %q gates its list API and its sitemap is the app.eightfold.ai decoy listing Eightfold's own board: not crawlable by either route", company)
		}

		locs = append(locs, loc)
	}

	if len(locs) == 0 {
		return nil, fmt.Errorf("Eightfold company %q gates its list API and its sitemap lists no job pages: not crawlable by either route", company)
	}

	if len(locs) > eightfoldSitemapMaxPostings {
		return nil, fmt.Errorf("refusing to walk the Eightfold sitemap for company %q: %d job pages at one request each is past the %d this route is budgeted for, and yielding a subset would report a partial board as complete", company, len(locs), eightfoldSitemapMaxPostings)
	}

	return locs, nil
}

// eightfoldJobLD is the subset of a job page's schema.org JobPosting JSON-LD
// block this adapter reads. baseSalary is deliberately absent: no gated tenant
// registered here published one when measured, and pay found in description
// prose goes through [internal.ParseCompensationFromDescription] instead.
type eightfoldJobLD struct {
	Type            string `json:"@type"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	DatePosted      string `json:"datePosted"`
	EmploymentType  string `json:"employmentType"`
	JobLocationType string `json:"jobLocationType"`
	JobLocation     struct {
		Address struct {
			Locality string          `json:"addressLocality"`
			Region   string          `json:"addressRegion"`
			Country  json.RawMessage `json:"addressCountry"`
		} `json:"address"`
	} `json:"jobLocation"`
}

// eightfoldLDCountry reads schema.org's addressCountry, which the spec allows as
// either a bare string or a Country object with a name.
func eightfoldLDCountry(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}

	var country struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &country) == nil {
		return strings.TrimSpace(country.Name)
	}

	return ""
}

// eightfoldLDTime parses a JSON-LD timestamp. Eightfold emits zone-less ISO
// 8601 ("2026-07-27T13:21:22") on every tenant measured; the RFC 3339 form is
// accepted too in case a tenant configures one. Stored as UTC like every other
// date in this project.
func eightfoldLDTime(value string) time.Time {
	value = strings.TrimSpace(value)

	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// eightfoldExternalIDFromLoc returns the numeric position id from a sitemap job
// URL, which is the leading digits of the path segment after /careers/job/:
// .../careers/job/563431013427445-software-support-specialist-... -> 563431013427445.
func eightfoldExternalIDFromLoc(loc string) string {
	_, rest, ok := strings.Cut(loc, "/careers/job/")
	if !ok {
		return ""
	}

	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}

	return rest[:end]
}

// eightfoldLDBlocks returns the parsed JSON-LD JobPosting blocks on a page.
func eightfoldLDBlocks(doc *html.Node) []eightfoldJobLD {
	var (
		blocks []eightfoldJobLD
		walk   func(*html.Node)
	)

	walk = func(n *html.Node) {
		if n == nil {
			return
		}

		if n.Type == html.ElementNode && n.Data == "script" {
			for _, attr := range n.Attr {
				if strings.EqualFold(attr.Key, "type") && strings.EqualFold(attr.Val, "application/ld+json") {
					var text strings.Builder
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.TextNode {
							text.WriteString(c.Data)
						}
					}

					var block eightfoldJobLD
					if err := json.Unmarshal([]byte(text.String()), &block); err == nil && block.Type == "JobPosting" {
						blocks = append(blocks, block)
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)

	return blocks
}

// eightfoldJobPage fetches one sitemap job page and reads its JSON-LD.
//
// The fetch goes to the tenant's {slug}.eightfold.ai host even when the sitemap
// published a branded host, because the branded hosts (careers.deere.com,
// apply.hp.com, ...) are all fronts for the same Eightfold backend with nothing
// textually in common — exactly the Radancy shape — and only the eightfold.ai
// suffix is covered by the shared pacing key in httpx. Measured 2026-07-30: the
// eightfold.ai host serves the identical page and JSON-LD for the same path.
// The posting's published URL stays the sitemap's canonical loc.
//
// ok is false, with no error, for a page that answered 404 or 410: sitemaps lag
// the board, a posting can close between the sitemap fetch and its page fetch,
// and a closed posting is an ordinary absence rather than a failed source. It
// is also false for a 200 page carrying no JobPosting block, which is how a
// trimble-shaped tenant (client-rendered pages, no JSON-LD at all) surfaces;
// the caller turns all-pages-empty into an error.
func eightfoldJobPage(ctx context.Context, httpClient *http.Client, company, loc string) (posting *internal.JobPosting, ok bool, err error) {
	parsed, err := url.Parse(loc)
	if err != nil {
		return nil, false, nil
	}

	fetchURL := "https://" + company + ".eightfold.ai" + parsed.EscapedPath()
	if parsed.RawQuery != "" {
		fetchURL += "?" + parsed.RawQuery
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request for Eightfold company %q: %w", company, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to make request to Eightfold for company %q: %w", company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status code from Eightfold for company %q at %s: %s", company, fetchURL, resp.Status)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse HTML from Eightfold for company %q: %w", company, err)
	}

	blocks := eightfoldLDBlocks(doc)
	if len(blocks) == 0 {
		return nil, false, nil
	}

	block := blocks[0]

	address := block.JobLocation.Address

	// Joined most-specific first, skipping parts already present in an earlier
	// one: amdocs publishes region "Uusimaa,FI" with country "FI", and joining
	// blindly would render "HKI, Uusimaa,FI, FI".
	parts := make([]string, 0, 3)
	for _, part := range []string{address.Locality, address.Region, eightfoldLDCountry(address.Country)} {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}

		redundant := false
		for _, earlier := range parts {
			if strings.Contains(strings.ToLower(earlier), strings.ToLower(part)) {
				redundant = true

				break
			}
		}

		if !redundant {
			parts = append(parts, part)
		}
	}

	location := strings.Join(parts, ", ")
	if location == "" {
		location = "unknown/remote"
	}

	posting = &internal.JobPosting{
		Company:  company,
		URL:      loc,
		Title:    strings.TrimSpace(block.Title),
		Location: location,
		PostedAt: eightfoldLDTime(block.DatePosted),
		Source: internal.PostingSource{
			Platform: eightfoldPlatform,
			Key:      company,
		},
	}

	if id := eightfoldExternalIDFromLoc(loc); id != "" {
		posting.ExternalID = id
	}

	if employment, ok := internal.NormalizeEmploymentType(block.EmploymentType); ok {
		posting.EmploymentType = employment
	}

	// schema.org's own remote marker. The same asymmetry as [eightfoldRemote]:
	// TELECOMMUTE sets the flag, anything else stays unset so the location text
	// heuristic keeps working.
	if strings.EqualFold(strings.TrimSpace(block.JobLocationType), "TELECOMMUTE") {
		posting.WorkplaceType = internal.WorkplaceTypeRemote
		posting.Remote = eightfoldRemote(internal.WorkplaceTypeRemote)
	}

	posting.Compensation = internal.ParseCompensationFromDescription(block.Description)

	return posting, true, nil
}

// eightfoldSitemapWalk crawls a gated tenant through its public sitemap, one
// job-page request per posting.
//
// This is the expensive route — the 22 tenants registered on it measured 4,051
// postings, so roughly 4,100 requests per crawl against ~410 if the list API
// ever opens up for them — and it is only entered after the list API answered
// the PCSX wall, never by preference.
func eightfoldSitemapWalk(ctx context.Context, httpClient *http.Client, company string, yield func(*internal.JobPosting, error) bool) {
	locs, err := eightfoldSitemapLocs(ctx, httpClient, company)
	if err != nil {
		yield(nil, err)

		return
	}

	var (
		seen      = make(map[string]bool, len(locs))
		attempted = 0
		yielded   = 0
	)

	for _, loc := range locs {
		if ctx.Err() != nil {
			yield(nil, ctx.Err())

			return
		}

		if seen[loc] {
			continue
		}

		seen[loc] = true

		posting, ok, err := eightfoldJobPage(ctx, httpClient, company, loc)
		if err != nil {
			yield(nil, err)

			return
		}

		attempted++

		if !ok {
			continue
		}

		yielded++

		if !yield(posting, nil) {
			return
		}
	}

	// Every page fetched and none carried a JobPosting block: that is not a
	// board with nothing to say, it is a tenant whose job pages this route
	// cannot read (trimble renders them client-side with no JSON-LD at all),
	// and reporting zero postings would make it indistinguishable from an
	// employer that is not hiring.
	if attempted > 0 && yielded == 0 {
		yield(nil, fmt.Errorf("unexpected response shape from Eightfold for company %q: %d sitemap job pages fetched but none carried a schema.org JobPosting block", company, attempted))
	}
}

// eightfoldWorkplaceType maps Eightfold's work_location_option to the project's
// vocabulary.
//
// The values seen are "onsite", "hybrid" and "remote_local"; "remote_global"
// appears in Eightfold's own configuration. [internal.NormalizeWorkplaceType]
// already reads all four, since it matches "remote" as a substring and checks
// "hybrid" first, so this exists to keep an unrecognised future value from
// becoming anything other than unknown.
func eightfoldWorkplaceType(raw string) internal.WorkplaceType {
	workplace, ok := internal.NormalizeWorkplaceType(raw)
	if !ok {
		return internal.WorkplaceTypeUnknown
	}

	return workplace
}

// eightfoldRemote reports the structured remote flag for a workplace type, and
// deliberately answers only "yes" or "no comment".
//
// [internal.JobPosting.IsRemote] — which is what `--remote` filters on — reads
// this field if it is set and otherwise falls back to searching the location and
// title for words like "remote". Setting it to true where Eightfold says remote
// is a straight improvement: a posting flagged remote_local but located in a
// named city ("New York,New York,United States") is invisible to that text
// search today.
//
// Returning a pointer to false for onsite and hybrid would be the symmetric
// thing to do and is wrong here, because it would suppress the text fallback
// with a value Eightfold does not actually stand behind. Measured on the
// registered tenants: a Netflix posting located "USA - Remote" is flagged
// onsite, and a Liberty Mutual posting located "Remote, Remote, United States"
// is flagged hybrid. Answering false for those would hide two genuinely remote
// jobs that the heuristic finds today, and IsRemote's contract is to err toward
// inclusion because a false negative hides a job.
func eightfoldRemote(workplace internal.WorkplaceType) *bool {
	if workplace != internal.WorkplaceTypeRemote {
		return nil
	}

	remote := true

	return &remote
}
