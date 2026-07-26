package services

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// oracleCloudPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
//
// It is named for Oracle Cloud HCM rather than for "ORC" (Oracle Recruiting
// Cloud, the recruiting module inside it) because Oracle also sells Taleo, a
// completely different product with a completely different API that this project
// may well add later. Two Oracle platforms sharing one name in the output would
// be unfixable after the fact.
const oracleCloudPlatform = "oraclecloud"

func init() {
	registerBuiltin(oracleCloudPlatform, multiJobsFuncNamed(OracleCloud, OracleCloudTenants, oracleCloudCompanyName))
}

// oracleCloudPageSize is how many requisitions are asked for per request.
//
// The list response already carries title, primary location and posted date, so
// unlike every reference implementation of this API that this project's research
// looked at, no per-posting detail request is needed: those exist only to fetch
// description prose, which [internal.JobPosting] does not store. That makes this
// roughly one request per 200 postings — the second-cheapest lane in the project
// after SuccessFactors, and the reason it is worth adding to a crawl that
// already misses its deadline.
const oracleCloudPageSize = 200

// oracleCloudMaxPages bounds how many pages a single tenant may be asked for.
//
// The repo has just finished repairing eight adapters that ended their loops
// only on a short or empty page: a tenant that answers every offset with the
// same first page never sends one, and the loop then runs until the crawl
// deadline, pinning a worker and hammering one host, while [internal.Dedupe]
// hides the duplicates so the posting total looks normal. [pageRepeatGuard]
// catches that case; this is the backstop for a tenant that keeps serving
// different full pages forever. At 200 per page it allows 100,000 postings from
// one tenant, roughly six times the largest in [OracleCloudTenants] (Kroger,
// ~16,300).
const oracleCloudMaxPages = 500

// OracleCloudTenants holds the Oracle Recruiting Cloud career sites this project
// crawls, one "slug,faHost,siteNumber" triple per entry.
//
// Tenancy is a triple and none of the three parts is guessable. faHost is the
// tenant's Fusion Applications host, which comes in several unrelated shapes
// ("eluq.fa.us2.oraclecloud.com", "fa-evxo-saasfaprod1.fa.ocs.oraclecloud.com",
// "jpmc.fa.oraclecloud.com"); siteNumber identifies one careers site within that
// tenant and is usually "CX_N" but is a name on plenty of them ("AEO-Careers",
// "PenskeCareers", "jobsearch"). Both are read off the employer's public careers
// page. The slug is this project's own name for the employer.
//
// # Why this list is short
//
// The research pass behind this adapter recovered 1,552 candidate triples, and
// this container cannot reach a job board, so none of them could be probed here.
// Registering all 1,552 unverified would be reckless at this project's fan-out:
// dead tenants burn a request each per crawl, report as failing sources, and
// enough of them together would trip the Source Health workflow's 35%-failure
// alarm — the signal that is supposed to mean a real platform broke.
//
// So this is a staging subset, and the full candidate list is committed verbatim
// with its provenance headers at testdata/candidates/oracle_orc_tenants.txt.
// Promoting the rest is mechanical work for a CI verification pass, the only
// place in this project with real network access: probe each triple, keep the
// ones whose list endpoint answers with a TotalJobsCount above zero, move them
// here. This adapter does not change when that happens.
//
// The 30 entries below are the highest-volume employers from the candidate
// file's hand-curated, industry-grouped head, whose header records per-triple
// verification (an og:site_name identity match plus TotalJobsCount > 0). The
// file's later sections — three automated apply-URL harvests, most of whose
// entries are annotated "[resolved needs_human]" — are excluded wholesale. The
// ordering is postings per HTTP request, which is the metric that matters for a
// crawl that already cannot finish. Two of these close gaps this project has
// recorded for years: Mayo Clinic and Kroger.
var OracleCloudTenants = []string{
	"kroger,eluq.fa.us2.oraclecloud.com,CX_2001",
	"autozone,egud.fa.us2.oraclecloud.com,CX_1",
	"jpmorgan,jpmc.fa.oraclecloud.com,CX_1001",
	"albertsons,eofd.fa.us6.oraclecloud.com,CX_1001",
	"sallybeauty,eigx.fa.us6.oraclecloud.com,CX_2",
	"lifepoint,ibnjjb.fa.ocs.oraclecloud.com,CX_1",
	"aeo,hcml.fa.us2.oraclecloud.com,AEO-Careers",
	"macys,ebwh.fa.us2.oraclecloud.com,CX_1001",
	"tenet,eodr.fa.us2.oraclecloud.com,CX_1",
	"chs,fa-evxo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"hilton,efet.fa.us2.oraclecloud.com,CX_1",
	"ihg,fa-evax-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"vertiv,egup.fa.us2.oraclecloud.com,CX",
	"encompasshealth,ibwsjb.fa.ocs.oraclecloud.com,CX_1",
	"abm,eiqg.fa.us2.oraclecloud.com,CX_1001",
	"brookdale,ibmwjb.fa.ocs.oraclecloud.com,CX_1",
	"quest,hdox.fa.us6.oraclecloud.com,CX_1",
	"providence,evac.fa.us2.oraclecloud.com,CX_1",
	"bny,eofe.fa.us2.oraclecloud.com,CX_1",
	"caesars,edmn.fa.us2.oraclecloud.com,CX_1",
	"waste-management,emcm.fa.us2.oraclecloud.com,CX_4001",
	"clubcorp,ecwl.fa.us2.oraclecloud.com,CX",
	"oracle,eeho.fa.us2.oraclecloud.com,jobsearch",
	"northwell,eppr.fa.us2.oraclecloud.com,CX_2",
	"honeywell,ibqbjb.fa.ocs.oraclecloud.com,CX_1",
	"penske,fa-euyk-saasfaprod1.fa.ocs.oraclecloud.com,PenskeCareers",
	"adventisthealth,ecvz.fa.us2.oraclecloud.com,CX_1",
	"mayoclinic,fa-euwp-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"kpmgkgs,ejgk.fa.em2.oraclecloud.com,CX_3",
	"hiltongrandvacations,efuq.fa.us6.oraclecloud.com,HiltonGrandVacations",
}

// Deliberately not registered, though it is the second-largest candidate in the
// file: "marriott,ejwl.fa.us2.oraclecloud.com,CX_2" (~11,900 postings). Marriott
// is already crawled on Jibe (see [JibeCompanies]), and an employer on two
// platforms at once is counted twice — [internal.Dedupe] keys on URL, and the
// same job has a different URL on each platform, so nothing downstream would
// catch it. jobs_record.txt is a trend line across runs, and a 12,000-posting
// step change that reflects no hiring would be indistinguishable from one that
// does.
//
// This is a routing decision, not a rejection. Whoever can compare the two
// routes' live counts should pick one and delete the other; the research behind
// this adapter expects the Oracle route to be the more complete of the two,
// because it is the employer's own ATS rather than a career-site front end.

// oracleCloudTenant is one parsed entry of [OracleCloudTenants].
type oracleCloudTenant struct {
	// key is the entry exactly as registered, which is what [Source.Key] and
	// [internal.PostingSource.Key] carry.
	key string

	// slug is this project's name for the employer.
	slug string

	// host is the tenant's Fusion Applications host.
	host string

	// site is the careers site within the tenant: "CX_1001", "AEO-Careers".
	site string
}

// parseOracleCloudTenant splits a "slug,faHost,siteNumber" key.
//
// A malformed entry is an error, not a default. The site number cannot be
// derived from the host, and a request built from two thirds of a triple would
// fail with an Oracle error far away from the mis-transcribed line that caused
// it.
func parseOracleCloudTenant(key string) (oracleCloudTenant, error) {
	parts := strings.Split(key, ",")
	if len(parts) != 3 {
		return oracleCloudTenant{}, fmt.Errorf("invalid Oracle Cloud tenant %q: want %q", key, "slug,faHost,siteNumber")
	}

	tenant := oracleCloudTenant{
		key:  key,
		slug: strings.TrimSpace(parts[0]),
		host: strings.TrimSpace(parts[1]),
		site: strings.TrimSpace(parts[2]),
	}

	if tenant.slug == "" || tenant.host == "" || tenant.site == "" {
		return oracleCloudTenant{}, fmt.Errorf("invalid Oracle Cloud tenant %q: want %q with all three parts set", key, "slug,faHost,siteNumber")
	}

	return tenant, nil
}

// oracleCloudCompanyName derives the display name from a tenant triple: the
// slug, which is the first field.
//
// A malformed entry returns unchanged so it stays traceable to the line that
// produced it, the same choice [workdayCompanyName] makes.
func oracleCloudCompanyName(key string) string {
	tenant, err := parseOracleCloudTenant(key)
	if err != nil {
		return key
	}

	return tenant.slug
}

// oracleCloudFinderEscape percent-encodes a "finder" parameter value, leaving
// the three characters that give it its structure alone.
//
// The finder is a small language of its own inside one query parameter:
// "findReqs;siteNumber=CX_1,limit=200,offset=0". Its semicolon, commas and
// equals signs are syntax, so [net/url.QueryEscape] — which escapes all three —
// produces a value Oracle answers with an error rather than with jobs. Every
// site number in [OracleCloudTenants] happens to need no escaping at all; this
// exists so that the first one that does (a space, an ampersand) fails to be a
// broken URL rather than failing to be escaped.
func oracleCloudFinderEscape(value string) string {
	const safe = "-_.~;,="

	var escaped strings.Builder

	escaped.Grow(len(value))

	for i := range len(value) {
		c := value[i]

		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			strings.IndexByte(safe, c) >= 0:
			escaped.WriteByte(c)
		default:
			fmt.Fprintf(&escaped, "%%%02X", c)
		}
	}

	return escaped.String()
}

// oracleCloudListURL builds the requisition list request for one page.
//
// The parameter set is kept exactly as the reference implementations this
// project's research read, including the "expand" of secondary locations that
// this adapter does not decode. That is a deliberate cost: the expand inflates
// the response for data that is discarded, but nothing here could be probed
// live, and a request that differs from the one known to work is a request that
// might quietly return nothing. Trimming it is a safe follow-up for whoever
// first verifies this platform from CI, not a guess to take now.
func oracleCloudListURL(tenant oracleCloudTenant, offset int) string {
	finder := fmt.Sprintf("findReqs;siteNumber=%s,limit=%d,offset=%d,sortBy=POSTING_DATES_DESC",
		tenant.site, oracleCloudPageSize, offset)

	return fmt.Sprintf("https://%s/hcmRestApi/resources/latest/recruitingCEJobRequisitions?onlyData=true&expand=requisitionList.secondaryLocations&finder=%s",
		tenant.host, oracleCloudFinderEscape(finder))
}

// oracleCloudPostingURL builds the public candidate-experience URL for one
// requisition.
//
// UNCERTAIN, and flagged as such deliberately: this route is the one part of
// this adapter that the research behind it did not document. It is the standard
// shape of an Oracle candidate-experience deep link and the site number in the
// path is the same one the finder is addressed by, but nobody has fetched one
// from this project. The blast radius is bounded and visible — a wrong template
// makes every link on the platform wrong in the same way, which is loud, rather
// than making postings disappear — and [internal.Dedupe] still works because the
// URL stays stable per posting. Verify it in the same CI pass that verifies the
// tenants.
func oracleCloudPostingURL(tenant oracleCloudTenant, id string) string {
	return fmt.Sprintf("https://%s/hcmUI/CandidateExperience/en/sites/%s/job/%s", tenant.host, tenant.site, id)
}

// oracleCloudResponse is the subset of the recruitingCEJobRequisitions response
// this adapter reads.
//
// The whole payload hangs off a single-element "items" array, with the total and
// the requisitions as siblings inside it — an artefact of the Fusion REST
// framework rather than a shape anyone would design.
type oracleCloudResponse struct {
	Items []struct {
		// TotalJobsCount is how many requisitions the site has in total, which
		// is what makes offset paging terminate on a count rather than on a
		// short page.
		//
		// Typed `any` for the same reason as the fields on
		// [oracleCloudRequisition]: it is decoded from documentation rather than
		// from a response anybody read here, and a single field with an
		// unexpected JSON type fails the decode for the entire page, which loses
		// a whole tenant. `any` cannot fail a decode.
		TotalJobsCount any `json:"TotalJobsCount"`

		RequisitionList []oracleCloudRequisition `json:"requisitionList"`
	} `json:"items"`
}

// oracleCloudRequisition is one opening in the list response.
//
// Everything here rides in the page the adapter already downloads, so decoding
// it costs no request and no measurable bytes.
//
// The typing rule is the one this package learned the hard way when Jibe's
// "meta_data" turned out to be an object on some tenants and a bare `false` on
// others, which silently disabled nine large employers: a field whose JSON type
// nobody has confirmed against a real response is `any`, read through [anyText].
// Title and PrimaryLocation are the exception — two independent implementations
// agree they are strings, and a title arriving as a number is not a failure mode
// worth trading readability for.
type oracleCloudRequisition struct {
	// Id is the requisition's identifier within the tenant, and the id the
	// candidate-experience URL is keyed by. It arrives quoted in Oracle's own
	// detail finder, which suggests a string, but that is an inference.
	ID any `json:"Id"`

	Title string `json:"Title"`

	PrimaryLocation string `json:"PrimaryLocation"`

	PostedDate any `json:"PostedDate"`

	// JobType is Oracle's employment-type picklist, "Full time" / "Part time" on
	// the tenants the research saw. Normalized through
	// [internal.NormalizeEmploymentType], so a spelling this project does not
	// recognise leaves the field empty rather than guessing.
	JobType any `json:"JobType"`

	// WorkplaceTypeCode is Oracle's genuine three-state workplace field:
	// ORA_REMOTE, ORA_HYBRID, ORA_ONSITE. The research found it documented on
	// the *detail* endpoint, so it may well be absent here — it is decoded
	// opportunistically, exactly as greenhouse.go decodes "first_published" from
	// a list that does not promise it. Tenants that do send it get a real
	// workplace type for free; the rest leave it empty, which is what
	// [internal.WorkplaceTypeUnknown] means.
	WorkplaceTypeCode any `json:"WorkplaceTypeCode"`

	// JobFunction is likewise a detail-endpoint field in the research and is
	// read opportunistically as this project's department.
	JobFunction any `json:"JobFunction"`
}

// oracleCloudLabel renders a value the API publishes as human-readable text,
// which is [anyText] minus the types that cannot be a label.
//
// anyText renders a bare `false` as the string "false" deliberately, because
// BambooHR publishes booleans that mean something. A department or a picklist
// code is a name, and Jibe's "meta_data" is this project's standing proof that a
// field which is an object on some tenants arrives as a bare `false` on others.
// Publishing "false" as an employer's department would be visible nonsense in
// every output format, so a non-textual value is treated as absent.
//
// A single-element array is still unwrapped, because a board that publishes a
// bare value on most tenants and a list of them on others is the ordinary case
// anyText was written for.
func oracleCloudLabel(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		if len(typed) > 0 {
			return oracleCloudLabel(typed[0])
		}
	}

	return ""
}

// oracleCloudDateLayouts are the timestamp spellings accepted for PostedDate.
//
// Only unambiguous ones, for the reason [phenomDateLayouts] spells out: a
// slash-separated date is a different day in the US and in Europe, and nothing
// in this response says which a tenant means. A date a month wrong would sit
// inside [internal.Filter.PostedSince] where nothing downstream could notice it.
var oracleCloudDateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// oracleCloudPostedAt converts a requisition's PostedDate to UTC, reporting
// false when it is missing or in a spelling this does not know.
func oracleCloudPostedAt(raw any) (time.Time, bool) {
	text := anyText(raw)
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range oracleCloudDateLayouts {
		if posted, err := time.Parse(layout, text); err == nil {
			return posted.UTC(), true
		}
	}

	return time.Time{}, false
}

// oracleCloudTotal reads TotalJobsCount, reporting false when the field was
// absent or unreadable.
//
// The two results are kept apart on purpose. "The site reports zero open reqs"
// is a legitimate answer that ends the crawl of a tenant quietly; "the response
// carried no total at all" means the shape is not the one this adapter was
// written against, and that has to be loud. Collapsing them into a plain 0 is
// what would turn a renamed field into a silently-empty source.
func oracleCloudTotal(raw any) (int, bool) {
	text := anyText(raw)
	if text == "" {
		return 0, false
	}

	total, err := strconv.Atoi(text)
	if err != nil || total < 0 {
		return 0, false
	}

	return total, true
}

// OracleCloud returns all of the job postings for one Oracle Recruiting Cloud
// careers site, or an error if there was a problem making a request or reading a
// response.
//
// company is a "slug,faHost,siteNumber" triple, see [OracleCloudTenants]; it is
// not a board slug like most platforms here.
func OracleCloud(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		tenant, err := parseOracleCloudTenant(company)
		if err != nil {
			yield(nil, err)

			return
		}

		var (
			pages  pageRepeatGuard
			offset int
		)

		for page := range oracleCloudMaxPages {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			// fetchJSON closes the response body before it returns, so a
			// thousand-page tenant cannot accumulate open bodies, and its errors
			// already name the platform and the tenant.
			doc, err := fetchJSON[oracleCloudResponse](ctx, httpClient, "Oracle Cloud", tenant.slug, jsonRequest{
				URL: oracleCloudListURL(tenant, offset),
			})
			if err != nil {
				yield(nil, err)

				return
			}

			// The envelope is always one item, even for a site with no jobs. An
			// empty items array is Oracle answering something other than this
			// API — a maintenance page, a site number that does not exist — and
			// reporting it as an employer with no openings is precisely the
			// silent failure this project fears most.
			if len(doc.Items) == 0 {
				yield(nil, fmt.Errorf("unexpected response from Oracle Cloud for company %q (site %s on %s): no items in the requisition list response, so the site number may be wrong or the API may have changed",
					tenant.slug, tenant.site, tenant.host))

				return
			}

			item := doc.Items[0]
			total, totalOK := oracleCloudTotal(item.TotalJobsCount)

			if len(item.RequisitionList) == 0 {
				// A first page with neither requisitions nor a readable total is
				// a shape this adapter does not recognise, not an empty board.
				if page == 0 && !totalOK {
					yield(nil, fmt.Errorf("unexpected response from Oracle Cloud for company %q (site %s on %s): the response carried neither a requisition list nor a job count, so its layout may have changed",
						tenant.slug, tenant.site, tenant.host))
				}

				return
			}

			ids := make([]string, 0, len(item.RequisitionList))
			for _, requisition := range item.RequisitionList {
				ids = append(ids, anyText(requisition.ID))
			}

			// Checked before anything is yielded, so a tenant that ignores
			// "offset" costs one wasted request rather than an endless stream of
			// duplicates.
			if pages.repeated(ids) {
				return
			}

			for _, requisition := range item.RequisitionList {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())

					return
				}

				posting := oracleCloudPosting(tenant, requisition)
				if posting == nil {
					continue
				}

				if !yield(posting, nil) {
					return
				}
			}

			// The offset advances by what the page actually held, not by what was
			// asked for. Several boards in this ecosystem answer with fewer rows
			// than the requested limit and still expect the caller to keep
			// walking — ADP's public API is documented to do exactly that — and an
			// offset stepped by the page size would then skip every row past the
			// first page's worth.
			offset += len(item.RequisitionList)

			if totalOK {
				// The site's own count is the authority when it published one. A
				// short page is not evidence of the end here: a tenant whose
				// server caps its page size below the requested limit would
				// otherwise stop at that cap, silently publishing a fraction of a
				// 16,000-posting employer with no error anywhere.
				if offset >= total {
					return
				}

				continue
			}

			if len(item.RequisitionList) < oracleCloudPageSize {
				return
			}
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Oracle Cloud for company %q (site %s on %s): the site was still serving full pages after %d pages of %d",
			tenant.slug, tenant.site, tenant.host, oracleCloudMaxPages, oracleCloudPageSize))
	}
}

// oracleCloudPosting builds one posting from a requisition, returning nil when
// the requisition carries too little to be one.
func oracleCloudPosting(tenant oracleCloudTenant, requisition oracleCloudRequisition) *internal.JobPosting {
	id := anyText(requisition.ID)

	// Without an id there is no link to the posting, and this project's contract
	// is that every posting carries a URL a person can open.
	if id == "" {
		return nil
	}

	location := strings.TrimSpace(requisition.PrimaryLocation)
	if location == "" {
		location = "unknown/remote"
	}

	posting := &internal.JobPosting{
		Company:  tenant.slug,
		URL:      oracleCloudPostingURL(tenant, id),
		Title:    strings.TrimSpace(requisition.Title),
		Location: location,

		Department: oracleCloudLabel(requisition.JobFunction),

		// The list publishes one identifier and it is the ATS's, the key the
		// candidate-experience route is addressed by. The employer's own
		// requisition number is a separate field on this platform and is not
		// corroborated as present in the list response, so RequisitionID is left
		// empty rather than filled with something that is not one.
		ExternalID: id,

		Source: internal.PostingSource{
			Platform: oracleCloudPlatform,
			Key:      tenant.key,
		},
	}

	// An unrecognised spelling leaves the field empty rather than guessing: a
	// wrong employment type cannot be told apart from a right one by a filter,
	// while an absent one is visibly absent.
	if employment, ok := internal.NormalizeEmploymentType(oracleCloudLabel(requisition.JobType)); ok {
		posting.EmploymentType = employment
	}

	// ORA_REMOTE / ORA_HYBRID / ORA_ONSITE all normalize on the word they
	// contain, so Oracle's prefix needs no special case here.
	if workplace, ok := internal.NormalizeWorkplaceType(oracleCloudLabel(requisition.WorkplaceTypeCode)); ok {
		posting.WorkplaceType = workplace
	}

	if posted, ok := oracleCloudPostedAt(requisition.PostedDate); ok {
		posting.PostedAt = posted
	}

	return posting
}
