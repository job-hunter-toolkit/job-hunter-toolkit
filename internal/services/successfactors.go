package services

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// successFactorsPlatform is the ATS family this file registers, and the value
// that reaches [internal.PostingSource.Platform].
const successFactorsPlatform = "successfactors"

func init() {
	registerBuiltin(successFactorsPlatform, multiJobsFuncNamed(SuccessFactors, SuccessFactorsTenants, successFactorsCompanyName))
}

// successFactorsMaxResponseBytes bounds one tenant's feed.
//
// This adapter does not paginate: SAP's "Recruiting Marketing" (RMK) feed
// answers a single GET with an enterprise's entire open-req corpus, ~144
// postings per request in the research that motivated this file, and full HTML
// descriptions inline. That is what makes it the cheapest enterprise lane in the
// project, and it is also why there is no page loop here to put a ceiling on.
//
// The equivalent bound for a single-shot adapter is on the response instead. The
// whole body has to be held in memory to be scanned (see [successFactorsJob]),
// so a tenant that answers with a stream that never ends, or with something that
// is not this feed at all, would otherwise be free to consume a worker's memory
// for as long as the crawl runs. 32 MiB is roughly twenty times the largest
// tenant in [SuccessFactorsTenants] (CRH, ~1,984 postings) at a generous 16 KiB
// of description each, and hitting it is reported as an error rather than parsed
// as a short feed: a truncated XML document would otherwise silently drop the
// tail of an employer's postings.
const successFactorsMaxResponseBytes = 32 << 20

// SuccessFactorsTenants holds the SAP SuccessFactors RMK career sites this
// project crawls, one "slug,companyId,host" triple per entry.
//
// Tenancy here is a triple, not a slug, and none of the three parts is
// guessable. companyId is the ?company= value the career site was configured
// with and is case-sensitive ("nestleHRprdBX", "C0000159936P"); host is
// career{N}.successfactors.{com|eu}, where both the number and the TLD are
// tenant-specific — career5.successfactors.com is NXDOMAIN while
// career5.successfactors.eu is live. The slug is this project's own name for the
// employer and is what a person types after --company.
//
// # Why this list is short
//
// The research pass that produced this adapter recovered 744 candidate triples,
// and this container has no outbound access to job boards, so not one of them
// could be probed here. Registering all 781 unverified would be reckless at this
// project's fan-out: a dead tenant burns a request per crawl, reports as a
// failing source, and enough of them together would trip the Source Health
// workflow's 35%-failure alarm — which is the signal that tells maintainers a
// real platform has broken. So this list is deliberately a staging subset.
//
// The full candidate list is committed verbatim, provenance headers and all, at
// testdata/candidates/successfactors_tenants.txt. Promoting the rest is a
// mechanical job for a CI verification pass, which is the only place in this
// project with real network access: probe each triple, keep the ones whose feed
// answers with a <Job-Listing> root and a non-zero <Job> count, and move them
// here. Nothing about this adapter changes when that happens.
//
// The 30 entries below were chosen from the candidate file's hand-curated head:
// three separate curation rounds whose header records that each triple was
// individually re-fetched and checked for an XML declaration, a <Job-Listing>
// root and a non-zero job count. The file's bulk sections — a 690-tenant
// wholesale import from a third-party instance registry, and three later
// apply-URL harvests — are excluded wholesale, not because they are wrong but
// because they were never individually inspected. Within the curated head these
// are the highest posting counts, which is also the ordering that matters for a
// crawl already missing its deadline: postings per HTTP request.
var SuccessFactorsTenants = []string{
	"crh,CRH,career2.successfactors.eu",
	"capgemini,capgemitecP3,career5.successfactors.eu",
	"sap,SAP,career5.successfactors.eu",
	"nestle,nestleHRprdBX,career2.successfactors.eu",
	"cargill,cargill,career2.successfactors.eu",
	"adidas,AdidasP,career5.successfactors.eu",
	"bombardier,Bombardier,career5.successfactors.eu",
	"dsv,dsvas,career2.successfactors.eu",
	"halliburton,HALprod,career4.successfactors.com",
	"atos,Atos,career5.successfactors.eu",
	"zurich,SF2013,career2.successfactors.eu",
	"boehringer,BoehringerPRD,career5.successfactors.eu",
	"exxonmobil,exxonmobilP,career4.successfactors.com",
	"basf,C0000159936P,career5.successfactors.eu",
	"bbraun,bbraunprd,career5.successfactors.eu",
	"schlumberger,Schlumberger,career2.successfactors.eu",
	"gulfstream,GulfStrProd,career4.successfactors.com",
	"ericsson,Ericsson,career2.successfactors.eu",
	"merckgroup,merckgroup,career5.successfactors.eu",
	"schaeffler,schaeffler,career5.successfactors.eu",
	"andritz,andritzag,career2.successfactors.eu",
	"bmw,bmwag,career5.successfactors.eu",
	"givaudan,givaudan,career5.successfactors.eu",
	"bertelsmann,Bertelsmann,career5.successfactors.eu",
	"colgate,colgate,career4.successfactors.com",
	"akzonobel,akzonobelsP2,career5.successfactors.eu",
	"voith,VOITH,career5.successfactors.eu",
	"schindler,Schindler,career5.successfactors.eu",
	"mahle,mahleinter,career5.successfactors.eu",
	"jbs,jbsaustral,career10.successfactors.com",
}

// successFactorsTenant is one parsed entry of [SuccessFactorsTenants].
type successFactorsTenant struct {
	// key is the entry exactly as registered, which is what [Source.Key] and
	// [internal.PostingSource.Key] carry. Kept verbatim rather than rebuilt from
	// the parts below, so the identity a posting reports is the one a person can
	// paste back into --company.
	key string

	// slug is this project's name for the employer, and the only part of the
	// triple a person ever types.
	slug string

	// companyID is the ?company= value, case-sensitive.
	companyID string

	// host is the career{N}.successfactors.{com|eu} host serving this tenant.
	host string
}

// parseSuccessFactorsTenant splits a "slug,companyId,host" key.
//
// A malformed entry is an error rather than a best-effort guess. The three parts
// are independent facts about a tenant that cannot be derived from each other,
// so a two-part key is not a tenant missing a default, it is a mis-transcribed
// line — and building a URL from it would produce a request that fails somewhere
// far away from the typo.
func parseSuccessFactorsTenant(key string) (successFactorsTenant, error) {
	parts := strings.Split(key, ",")
	if len(parts) != 3 {
		return successFactorsTenant{}, fmt.Errorf("invalid SuccessFactors tenant %q: want %q", key, "slug,companyId,host")
	}

	tenant := successFactorsTenant{
		key:       key,
		slug:      strings.TrimSpace(parts[0]),
		companyID: strings.TrimSpace(parts[1]),
		host:      strings.TrimSpace(parts[2]),
	}

	if tenant.slug == "" || tenant.companyID == "" || tenant.host == "" {
		return successFactorsTenant{}, fmt.Errorf("invalid SuccessFactors tenant %q: want %q with all three parts set", key, "slug,companyId,host")
	}

	return tenant, nil
}

// successFactorsCompanyName derives the display name from a tenant triple: the
// slug, which is the first field.
//
// It returns the key unchanged when the triple is malformed, so a bad entry
// stays traceable back to the line that produced it rather than becoming an
// empty name in the company list — the same choice [workdayCompanyName] makes.
func successFactorsCompanyName(key string) string {
	tenant, err := parseSuccessFactorsTenant(key)
	if err != nil {
		return key
	}

	return tenant.slug
}

// successFactorsFeedMarker is the root element of the RMK listing feed.
//
// Its presence is what tells a real feed apart from the small HTML page a wrong
// host or company id answers with. That page is served with a 200, so status
// code alone cannot make the distinction, and treating it as an empty board
// would turn every mis-transcribed tenant into a silently-empty source.
const successFactorsFeedMarker = "<Job-Listing"

// The RMK feed is deliberately scanned rather than parsed with encoding/xml.
//
// It is not strict XML: the runtime emits empty "<>...</>" tags for facets a
// tenant has not configured, which encoding/xml rejects outright. One such tag
// anywhere in the document would cost the whole tenant its postings, which is
// the silently-empty source this project treats as its worst failure. A scanner
// that looks only for the elements it needs cannot be broken by markup it does
// not read.
//
// Every pattern is non-greedy and anchored on a full tag name, so <Job> does not
// match <Job-Description> or <JobTitle>, and (?s) is what lets a CDATA
// description spanning thousands of lines be captured as one value.
var (
	successFactorsJobPattern         = regexp.MustCompile(`(?is)<Job\s*>(.*?)</Job\s*>`)
	successFactorsTitlePattern       = regexp.MustCompile(`(?is)<JobTitle\b[^>]*>(.*?)</JobTitle\s*>`)
	successFactorsDescriptionPattern = regexp.MustCompile(`(?is)<Job-Description\b[^>]*>(.*?)</Job-Description\s*>`)
	successFactorsReqIDPattern       = regexp.MustCompile(`(?is)<ReqId\b[^>]*>(.*?)</ReqId\s*>`)
	successFactorsPostedPattern      = regexp.MustCompile(`(?is)<Posted-Date\b[^>]*>(.*?)</Posted-Date\s*>`)

	// Facets are the tenant-variable part of the feed: each configured filter or
	// multi-value field arrives as <filterN>/<mfieldN> carrying a <label> and a
	// <value>, and which N holds the location differs per tenant. Matching the
	// family rather than a fixed number is the only way to read them without a
	// per-tenant mapping table.
	successFactorsFacetPattern = regexp.MustCompile(`(?is)<(?:filter|mfield)\d*\s*>(.*?)</(?:filter|mfield)\d*\s*>`)
	successFactorsLabelPattern = regexp.MustCompile(`(?is)<label\b[^>]*>(.*?)</label\s*>`)
	successFactorsValuePattern = regexp.MustCompile(`(?is)<value\b[^>]*>(.*?)</value\s*>`)

	// successFactorsMergeToken matches an RMK merge token such as
	// "[[salaryMin]]" or "[[filter1]]". These are placeholders the career site's
	// client-side runtime would have substituted; a plain HTTP client receives
	// them literally, and publishing one as a location or a title would put
	// template syntax in front of a job seeker.
	successFactorsMergeToken = regexp.MustCompile(`\[\[[^\]\[]*\]\]`)
)

// successFactorsText renders one captured element's contents as plain text.
//
// CDATA is unwrapped and its contents are left exactly as they arrived: CDATA
// exists precisely to hold text that must not be re-interpreted, so unescaping
// it would turn a literal "&amp;" in a job title into "&". A value that is not
// CDATA-wrapped is entity-decoded, because for those the encoding is real.
func successFactorsText(raw string) string {
	text := strings.TrimSpace(raw)

	if after, ok := strings.CutPrefix(text, "<![CDATA["); ok {
		text = strings.TrimSuffix(after, "]]>")
	} else {
		text = html.UnescapeString(text)
	}

	text = successFactorsMergeToken.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// successFactorsElement returns the text of the first element matched by pattern
// within one <Job> block, or "" when the element is absent.
func successFactorsElement(pattern *regexp.Regexp, block string) string {
	match := pattern.FindStringSubmatch(block)
	if match == nil {
		return ""
	}

	return successFactorsText(match[1])
}

// successFactorsFacet is one label/value pair from a <filterN> or <mfieldN>
// wrapper.
type successFactorsFacet struct {
	label string
	value string
}

// successFactorsFacets reads every configured facet out of one <Job> block, in
// document order.
func successFactorsFacets(block string) []successFactorsFacet {
	matches := successFactorsFacetPattern.FindAllStringSubmatch(block, -1)

	facets := make([]successFactorsFacet, 0, len(matches))

	for _, match := range matches {
		facet := successFactorsFacet{
			label: strings.ToLower(successFactorsElement(successFactorsLabelPattern, match[1])),
			value: successFactorsElement(successFactorsValuePattern, match[1]),
		}

		// A facet the tenant left unconfigured arrives with an empty value, and
		// one with no label cannot be identified at all; neither is usable, and
		// keeping them would only make the lookups below scan further.
		if facet.label == "" || facet.value == "" {
			continue
		}

		facets = append(facets, facet)
	}

	return facets
}

// Facet labels are matched by substring, in the priority order given here.
//
// Which facet holds which fact is a per-tenant configuration choice, and there is
// nothing in the feed that says which convention a tenant follows: measured live
// on 2026-07-28, CRH labels its geography "Country" and "State/Province/County",
// Colgate labels it "Country", "State/Province" and "City", and Zurich labels it
// "Country of Search". Matching on the label the tenant chose to display is the
// only signal available.
//
// This is the least certain part of the adapter and it is deliberately the least
// load-bearing: a label that matches nothing here leaves an enrichment field
// empty, exactly as [internal.NormalizeEmploymentType] returning false does.
// Title, URL and the posting itself never depend on any of it.
var (
	// successFactorsWorkplaceLabels also acts as an exclusion list for
	// locations: "Location Flexibility" is a remote/hybrid picklist and contains
	// the word "location", so a plain substring search would file "Hybrid" as a
	// city.
	//
	// Deliberately WITHOUT "work location", which is the trap this list cannot
	// be the answer to. Colgate labels its remote/hybrid picklist "Work
	// Location", but of the eight tenants measured on 2026-07-28 that carry a
	// "... Location" label of that family, seven use it for real geography:
	// Cornell publishes "Upper East Side", Schindler "Boston", Voith
	// "Heidenheim, BW (DE)", Langan "Arlington, VA", Cincinnati "Main Campus".
	// Excluding the label would have cost those seven their locations to fix one
	// tenant. [successFactorsWorkplaceValue] handles Colgate by reading the
	// value instead, which is where the two cases actually differ.
	successFactorsWorkplaceLabels = []string{"work model", "workplace", "work arrangement", "location flexibility", "remote"}

	// Ordered most-complete-first: a tenant that publishes both "Location" and
	// "City" usually puts the fuller string in the former ("Ludwigshafen, DE"
	// against "Ludwigshafen"), and 46 of the 739 live tenants publish a bare
	// "Location" facet.
	successFactorsLocationLabels = []string{"geographic location", "location", "city", "region", "state", "province", "country"}

	// "job area" is Zurich's label for what every other measured tenant calls a
	// job function ("Claims", "Underwriting", "Information Technology"); five
	// live tenants use it, and no measured tenant uses it for geography.
	successFactorsDepartmentLabels = []string{"job function", "function", "department", "job family", "job category", "career area", "job area"}

	// Deliberately without "job type": Zurich publishes a "Job Type" facet whose
	// values are seniority levels ("Experienced", "Entry", "Graduate"), not
	// employment types, so matching it would file a seniority as a contract
	// shape on every posting that has one.
	successFactorsEmploymentLabels = []string{"employment type", "employment status", "contract type", "work schedule", "employment"}
)

// successFactorsFacetValue returns the value of the first facet whose label
// contains one of labels, honouring the order of labels rather than the order of
// the facets, so priority is a property of this file and not of a tenant's
// column layout.
func successFactorsFacetValue(facets []successFactorsFacet, labels []string, exclude []string) string {
	for _, want := range labels {
		for _, facet := range facets {
			if !strings.Contains(facet.label, want) {
				continue
			}

			if successFactorsLabelContains(facet.label, exclude) {
				continue
			}

			return facet.value
		}
	}

	return ""
}

// successFactorsLabelContains reports whether a lowercased facet label contains
// any of the given words.
func successFactorsLabelContains(label string, words []string) bool {
	for _, word := range words {
		if strings.Contains(label, word) {
			return true
		}
	}

	return false
}

// successFactorsISOLayouts are the unambiguous date spellings accepted for
// <Posted-Date>, tried before any slash-separated reading.
var successFactorsISOLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// successFactorsMonthFirst is the documented spelling of <Posted-Date>:
// MM/DD/YYYY.
const successFactorsMonthFirst = "01/02/2006"

// successFactorsDayFirst is the same date read the other way round.
const successFactorsDayFirst = "02/01/2006"

// successFactorsSlashLayout decides how to read this feed's slash-separated
// dates, from the whole feed rather than from one posting.
//
// 03/04/2026 is the third of April to half the world and the fourth of March to
// the other half, and [phenomPostedAt] refuses slash dates outright for exactly
// that reason. RMK is a narrower case: the format is documented as MM/DD/YYYY,
// and a feed carries every open req at once, so the corpus itself settles the
// question. A single value whose first component exceeds 12 can only be a day,
// which proves the tenant is day-first; across the ~144 postings a typical
// tenant publishes, a day-first tenant is very unlikely to hide.
//
// Falling back to the documented reading when no such value appears is the
// remaining risk, and it is bounded: it can only be wrong for a tenant that is
// day-first AND published nothing after the 12th of any month.
func successFactorsSlashLayout(dates []string) string {
	for _, date := range dates {
		first, _, ok := strings.Cut(date, "/")
		if !ok {
			continue
		}

		// Read as a number rather than with time.Parse: the layout "01" demands
		// two digits, so an unpadded "3/04/2026" would fail to parse and be
		// mistaken for proof of a day-first tenant.
		number, err := strconv.Atoi(strings.TrimSpace(first))
		if err != nil {
			continue
		}

		// Only a day can exceed 12, so one such value settles the whole feed.
		if number > 12 {
			return successFactorsDayFirst
		}
	}

	return successFactorsMonthFirst
}

// successFactorsPostedAt converts one <Posted-Date> to UTC using the layout
// [successFactorsSlashLayout] settled on for this feed, reporting false when the
// field is absent or in a spelling this does not know.
func successFactorsPostedAt(raw, slashLayout string) (time.Time, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range successFactorsISOLayouts {
		if posted, err := time.Parse(layout, text); err == nil {
			return posted.UTC(), true
		}
	}

	if posted, err := time.Parse(slashLayout, text); err == nil {
		return posted.UTC(), true
	}

	return time.Time{}, false
}

// successFactorsFeed fetches one tenant's listing feed as text.
//
// It does not go through [fetchJSON]: the response is XML, and it has to be held
// as a whole because the scanner needs the complete document. The body is closed
// before this returns on every path, so a failed read cannot leave a connection
// pinned for the rest of the crawl.
func successFactorsFeed(ctx context.Context, httpClient *http.Client, tenant successFactorsTenant) (string, error) {
	feedURL := fmt.Sprintf("https://%s/career?company=%s&career_ns=job_listing_summary&resultType=XML",
		tenant.host, url.QueryEscape(tenant.companyID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for SuccessFactors company %q at %s: %w", tenant.slug, feedURL, err)
	}

	// The feed is served as application/octet-stream by most tenants rather than
	// as any XML media type, so this is an honest statement of what is accepted
	// rather than a filter the server is expected to honour.
	req.Header.Set("Accept", "application/xml, text/xml, */*")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request to SuccessFactors for company %q at %s: %w", tenant.slug, feedURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from SuccessFactors for company %q at %s: %s", tenant.slug, feedURL, resp.Status)
	}

	// One byte past the limit is read on purpose: it is what distinguishes a feed
	// that exactly fills the budget from one that was cut short.
	body, err := io.ReadAll(io.LimitReader(resp.Body, successFactorsMaxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response from SuccessFactors for company %q at %s: %w", tenant.slug, feedURL, err)
	}

	if len(body) > successFactorsMaxResponseBytes {
		return "", fmt.Errorf("refusing to read more than %d bytes from SuccessFactors for company %q at %s: the feed did not end, so its postings cannot be read without truncating them",
			successFactorsMaxResponseBytes, tenant.slug, feedURL)
	}

	return string(body), nil
}

// successFactorsApplyURL builds the public posting URL for a requisition.
//
// RMK publishes no per-posting link in the feed, but the application route is
// the same three parameters on the same host for every tenant, so the URL is
// synthesizable with no second request. That is the whole reason this platform
// costs one request per employer instead of one per posting.
func successFactorsApplyURL(tenant successFactorsTenant, reqID string) string {
	return fmt.Sprintf("https://%s/career?company=%s&career_job_req_id=%s&career_ns=job_application",
		tenant.host, url.QueryEscape(tenant.companyID), url.QueryEscape(reqID))
}

// SuccessFactors returns all of the job postings for one SAP SuccessFactors RMK
// tenant, or an error if there was a problem making the request or reading the
// feed.
//
// company is a "slug,companyId,host" triple, see [SuccessFactorsTenants]; it is
// not a board slug like most platforms here.
func SuccessFactors(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		tenant, err := parseSuccessFactorsTenant(company)
		if err != nil {
			yield(nil, err)

			return
		}

		body, err := successFactorsFeed(ctx, httpClient, tenant)
		if err != nil {
			yield(nil, err)

			return
		}

		// A wrong host or company id answers 200 with a short HTML page, so the
		// root element is the only thing that distinguishes "this tenant has no
		// open reqs" from "this tenant does not exist". Failing loudly here is
		// what keeps a mis-transcribed triple from looking like an employer with
		// nothing to offer.
		if !strings.Contains(body, successFactorsFeedMarker) {
			yield(nil, fmt.Errorf("unexpected response from SuccessFactors for company %q (company id %q on %s): no %s element, which is what a wrong host or company id answers with",
				tenant.slug, tenant.companyID, tenant.host, successFactorsFeedMarker))

			return
		}

		blocks := successFactorsJobPattern.FindAllStringSubmatch(body, -1)
		if len(blocks) == 0 {
			// The feed is real and lists nothing. An enterprise with no open
			// reqs is unusual but not an error, and the marker above has already
			// ruled out the failure that looks like this one.
			return
		}

		dates := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if posted := successFactorsElement(successFactorsPostedPattern, block[1]); posted != "" {
				dates = append(dates, posted)
			}
		}

		slashLayout := successFactorsSlashLayout(dates)

		var yielded int

		for _, block := range blocks {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			posting := successFactorsJob(tenant, block[1], slashLayout)
			if posting == nil {
				continue
			}

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// Every element name this scanner looks for came from documentation and
		// from other people's implementations, never from a response decoded
		// here. If RMK renames <JobTitle> or <ReqId>, or a tenant serves a shape
		// nobody has seen, the failure mode without this check is a tenant that
		// reports success and contributes nothing — the exact shape of failure
		// that cost this project two thirds of its coverage before. So a feed
		// that listed jobs and yielded none is an error, loudly, naming the
		// tenant.
		if yielded == 0 {
			yield(nil, fmt.Errorf("failed to read any posting from SuccessFactors for company %q (company id %q on %s): the feed listed %d jobs but none carried both a title and a requisition id, so its layout may have changed",
				tenant.slug, tenant.companyID, tenant.host, len(blocks)))
		}
	}
}

// successFactorsJob builds one posting from a <Job> block, returning nil when
// the block carries too little to be a posting.
func successFactorsJob(tenant successFactorsTenant, block, slashLayout string) *internal.JobPosting {
	var (
		title = successFactorsElement(successFactorsTitlePattern, block)
		reqID = successFactorsElement(successFactorsReqIDPattern, block)
	)

	// Without the requisition id there is no link to the posting, and this
	// project's contract is that every posting carries a URL a person can open.
	if title == "" || reqID == "" {
		return nil
	}

	facets := successFactorsFacets(block)

	location := successFactorsFacetValue(facets, successFactorsLocationLabels, successFactorsWorkplaceLabels)
	if location == "" {
		location = "unknown/remote"
	}

	posting := &internal.JobPosting{
		Company:  tenant.slug,
		URL:      successFactorsApplyURL(tenant, reqID),
		Title:    title,
		Location: location,

		Department: successFactorsFacetValue(facets, successFactorsDepartmentLabels, nil),

		// RMK publishes one identifier, and it is both: the number the employer
		// quotes in an internal system and the key the application route is
		// addressed by. Filling only one of the two fields would make callers
		// guess which, so both carry it and the doc comments on
		// [internal.JobPosting] explain what each means.
		RequisitionID: reqID,
		ExternalID:    reqID,

		Source: internal.PostingSource{
			Platform: successFactorsPlatform,
			Key:      tenant.key,
		},
	}

	if employment, ok := internal.NormalizeEmploymentType(successFactorsFacetValue(facets, successFactorsEmploymentLabels, nil)); ok {
		posting.EmploymentType = employment
	}

	if workplace, ok := internal.NormalizeWorkplaceType(successFactorsFacetValue(facets, successFactorsWorkplaceLabels, nil)); ok {
		posting.WorkplaceType = workplace
	}

	if posted, ok := successFactorsPostedAt(successFactorsElement(successFactorsPostedPattern, block), slashLayout); ok {
		posting.PostedAt = posted
	}

	// The description is already on the wire — it is the bulk of this feed — so
	// reading a pay range out of it costs no request. It arrives as HTML, which
	// [internal.ParseCompensationFromDescription] handles: it strips tags and
	// decodes entities before looking for figures. Anything it finds is marked
	// [internal.ProvenanceDescription], never confused with an employer-published
	// field, and RMK publishes no structured pay field for it to displace.
	posting.Compensation = internal.ParseCompensationFromDescription(successFactorsElement(successFactorsDescriptionPattern, block))

	return posting
}
