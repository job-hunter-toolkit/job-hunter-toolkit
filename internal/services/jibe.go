package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// jibePlatform is the ATS family this file registers, and the value that reaches
// [internal.PostingSource.Platform].
const jibePlatform = "jibe"

func init() {
	registerBuiltin(jibePlatform, multiJobsFuncNamed(Jibe, JibeCompanies, jibeCompanyName))
}

// jibePageSize is the number of postings requested per page.
const jibePageSize = 100

// jibeMaxPages bounds how many pages a single Jibe tenant may be asked for.
//
// "totalCount" and [pageRepeatGuard] are the real stops; this is the backstop
// for a tenant that reports no usable total and keeps serving different full
// pages forever, which is how the unbounded loop this replaces ran up 5,001
// requests and 500,001 duplicate postings against a stub in 0.8s. At 100
// postings per page it still allows 200,000 postings from one company, well
// above the largest Jibe boards here (FedEx, Costco, Marriott), so reaching it
// means the board is misbehaving and the adapter says so rather than crawling
// on.
const jibeMaxPages = 2000

var JibeCompanies = []string{
	"84lumber",
	"alaskaair",
	"amedisys",
	"ascension",
	"bjc",
	"brightspring",
	"carenewengland",
	"casella",
	"celanese",
	"chsli",
	"commonspirit",
	"conehealth",
	"costco",
	"cubesmart",
	"delawarenorth",
	"discounttire",
	"dollargeneral",
	"dunhamssports",
	"eagleview",
	"einstein",
	"emory",
	"exeloncorp",
	"farmersinsurance",
	"fedex",
	"footlocker",
	"generalmills",
	"githubinc",
	"gnc",
	"heb",
	"jcpenney",
	"marriott",
	"medstarhealth",
	"mercy",
	"merlinentertainments",
	"mountsinai",
	"naturalgrocers",
	"nfiindustries",
	"noodles",
	"novanthealth",
	"obhs",
	"ohsu",
	"orlandohealth",
	"paychex",
	"penfed",
	"pennentertainment",
	"pepsico",
	"petsmart",
	"piedmont",
	"redlobster",
	"rei",
	"riteaid",
	"rockefelleruniversity",
	"rush",
	"sheetz",
	"siteone",
	"sixflags",
	"sprouts",
	"statefarm",
	"stjude",
	"suncoastcreditunion",
	"sutterhealth",
	"thecheesecakefactory",
	"towerhealth",
	"ucla",
	"uhs",
	"ulta",
	"umms",
	"unitypoint",
	"wakemed",
	"wendys",
	"xanterra",

	// iCIMS rebuilt its modern career sites on Jibe and serves the identical
	// /api/jobs endpoint from the employer's OWN domain, so these are Jibe
	// boards that this adapter simply never asked for: it only ever built
	// "{slug}.jibeapply.com". The response is byte-for-byte the shape jibeJobs
	// already models, which is why this costs no new parsing code.
	//
	// These 60 are the highest-volume of 262 net-new hosts recovered by the
	// source survey, and carry about 70% of its annotated 166,519 postings.
	// The remaining 202 are staged unverified in
	// testdata/candidates/jibe_vanity_hosts.txt: nothing in this container can
	// reach a job board, so a host that has since been retired is
	// indistinguishable here from one that works, and registering all of them
	// blind would put that uncertainty straight into the health report.
	"aus.jibeapply.com",
	"careers.accentcare.com",
	"careers.alignmedpartners.com",
	"careers.amd.com",
	"careers.axa.com",
	"careers.bjsrestaurants.com",
	"careers.busybeeschildcare.co.uk",
	"careers.callnorthwest.com",
	"careers.clarkpest.com",
	"careers.cranepestcontrol.com",
	"careers.crittercontrol.com",
	"careers.fairview.org",
	"careers.ieaconstructors.com",
	"careers.indfumco.com",
	"careers.landrysinc.com",
	"careers.lemartec.com",
	"careers.mastec.com",
	"careers.masteccommunicationsgroup.com",
	"careers.mastecindustrial.com",
	"careers.mcdean.com",
	"careers.mymichigan.org",
	"careers.opcpest.com",
	"careers.orkin.com",
	"careers.permatreat.com",
	"careers.pestdefense.com",
	"careers.powerbackrehab.com",
	"careers.primehealthcare.com",
	"careers.publicisgroupe.com",
	"careers.radnet.com",
	"careers.rollins.com",
	"careers.se.com",
	"careers.sunriseseniorliving.com",
	"careers.trutechinc.com",
	"careers.walthamservices.com",
	"careers.wanzek.com",
	"careers.westernpest.com",
	"conduent.jibeapply.com",
	"highgate.jibeapply.com",
	"jobs.ajg.com",
	"jobs.aon.com",
	"jobs.ardenthealth.com",
	"jobs.firstwatch.com",
	"jobs.fraserhealth.ca",
	"jobs.jcp.com",
	"jobs.mastecat.com",
	"jobs.pdshealth.com",
	"jobs.trilogyhs.com",
	"jobs.ufhealth.org",
	"jobs.uhsinc.com",
	"jobs.ynhhs.org",
	"karriere.korian.de",
	"www.cakecareers.com",
	"www.foxrccareers.com",
	"www.genesiscareers.jobs",
	"www.grandluxcareers.com",
	"www.northitaliacareers.com",
}

// jibeJobs is the subset of Jibe's job search response that this adapter uses.
//
// The full response carries a large amount of additional metadata (Google Jobs
// derived info, facet lists, language counts). It is deliberately not modelled
// here: some fields are polymorphic across tenants, the top-level "meta_data"
// is an object for some companies and a bare `false` for others; so decoding
// them into fixed Go types made every such tenant fail to decode.
//
// That extra metadata is still unmodelled after the schema grew a department,
// an employment type and a posting date, and the reason is worth writing down
// rather than rediscovering. Nothing in this repository has ever decoded a live
// Jibe body beyond the four fields below, and this container cannot reach
// jibeapply.com to capture one. The names a schema.org-derived payload would
// plausibly use — employmentType, datePosted, jobLocation — are a guess, and a
// guessed *name* silently yields nothing while a guessed *type* fails the decode
// and takes the whole tenant with it. That is not hypothetical here: it is
// exactly what "meta_data" did to nine large employers, including the biggest
// source in the project. Capture one real response per tenant shape in Actions
// first (docs/adding-a-source.md), then model against it; the mapping itself is
// a handful of lines once the body is in hand.
type jibeJobs struct {
	Jobs []struct {
		Data struct {
			// Jibe is one of the few platforms that publishes pay as structured
			// numbers, and it populates them often, measured at 69 of 100
			// PetSmart postings. Frequency is an enum like "HOURLY"; currency is
			// frequently empty even when the amounts are present.
			SalaryMin       float64 `json:"salary_min_value"`
			SalaryMax       float64 `json:"salary_max_value"`
			SalaryCurrency  string  `json:"salary_currency"`
			SalaryFrequency string  `json:"salary_frequency"`

			Title        string `json:"title"`
			ApplyURL     string `json:"apply_url"`
			FullLocation string `json:"full_location"`
		} `json:"data,omitempty"`
	} `json:"jobs"`
	TotalCount int `json:"totalCount"`
}

// jibeFrequencies maps Jibe's pay frequency enum onto [internal.Period].
var jibeFrequencies = map[string]internal.Period{
	"HOURLY":  internal.PeriodHour,
	"DAILY":   internal.PeriodDay,
	"WEEKLY":  internal.PeriodWeek,
	"MONTHLY": internal.PeriodMonth,
	"YEARLY":  internal.PeriodYear,
	"ANNUAL":  internal.PeriodYear,
}

// jibeCompensation builds a pay range from Jibe's salary fields, returning nil
// when the tenant published no amounts.
//
// Jibe sends explicit zeros rather than omitting the fields, so a zero range
// means "not disclosed" and must not become a posting that claims to pay nothing.
func jibeCompensation(minValue, maxValue float64, currency, frequency string) *internal.Compensation {
	if minValue <= 0 && maxValue <= 0 {
		return nil
	}

	return &internal.Compensation{
		Min:        minValue,
		Max:        maxValue,
		Currency:   strings.ToUpper(strings.TrimSpace(currency)),
		Period:     jibeFrequencies[strings.ToUpper(strings.TrimSpace(frequency))],
		Provenance: internal.ProvenanceEmployer,
	}
}

// jibePage fetches a single page of Jibe postings. It exists so the response
// body is closed when the page is done rather than accumulating one open body
// per page for the lifetime of the whole crawl.
func jibePage(ctx context.Context, httpClient *http.Client, company, baseURL string, page int) (*jibeJobs, error) {
	query := url.Values{
		"page":  {strconv.Itoa(page)},
		"limit": {strconv.Itoa(jibePageSize)},
	}

	return fetchJSON[jibeJobs](ctx, httpClient, "Jibe", company, jsonRequest{
		URL: baseURL + "?" + query.Encode(),
	})
}

// jibeHost returns the host serving a Jibe key's board.
//
// A key containing a dot is an employer's own careers hostname and is used
// verbatim; a bare key is a jibeapply.com slug. Both exist because iCIMS
// rebuilt its modern career sites on Jibe and serves the identical
// /api/jobs endpoint from the EMPLOYER's domain: careers.costco.com,
// jobs.jcp.com, careers.se.com. This adapter only ever built
// "{key}.jibeapply.com", so every one of those employers was invisible to the
// crawl even though the response shape jibeJobs already models is byte-for-byte
// the same. The .icims.com host is not a substitute: it 404s on /api/jobs, so
// the vanity host is the only way in.
//
// The split is on a dot rather than on a registry of known vanity hosts so that
// adding one is a data change, not a code change, which is the same reason
// Workday keys on a tenant URL.
func jibeHost(key string) string {
	if strings.Contains(key, ".") {
		return key
	}

	return key + ".jibeapply.com"
}

// jibeCompanyName derives a readable company name from a Jibe key.
//
// Bare slugs are already readable. A vanity host is not: left alone it would put
// "careers.costco.com" in the company list, where it sorts under "c" for
// "careers" rather than Costco and makes --company costco silently match
// nothing. That exact failure is why Source keeps Key and Company separate.
func jibeCompanyName(key string) string {
	if !strings.Contains(key, ".") {
		return key
	}

	host := strings.TrimSuffix(key, ".")

	for _, prefix := range []string{"careers.", "career.", "jobs.", "job.", "www.", "apply.", "talent."} {
		if after, ok := strings.CutPrefix(host, prefix); ok {
			host = after

			break
		}
	}

	// Drop the public suffix, keeping the registrable label: "costco.com" and
	// "se.com" become "costco" and "se". Multi-label suffixes such as .co.uk
	// leave a two-label name, which is still recognisable and is preferable to
	// guessing at a public-suffix list this project does not vendor.
	if idx := strings.Index(host, "."); idx > 0 {
		host = host[:idx]
	}

	return host
}

// Jibe returns job postings from Jibe's API for a given company. It's unclear to me where this
// API is documented now, but it seems like it's still available even after the ICIIMS acquisition.
//
// https://www.icims.com/company/newsroom/icims-acquires-jibe-to-provide-employers-best-in-class-candidate-engagement-and-recruitment-marketing-capabilities/
func Jibe(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			baseURL = "https://" + jibeHost(company) + "/api/jobs"
			page    = 1
			pages   pageRepeatGuard
			fetched int
		)

		for range jibeMaxPages {
			apiResp, err := jibePage(ctx, httpClient, company, baseURL, page)
			if err != nil {
				yield(nil, err)
				return
			}

			if len(apiResp.Jobs) == 0 {
				return
			}

			ids := make([]string, 0, len(apiResp.Jobs))
			for _, item := range apiResp.Jobs {
				ids = append(ids, item.Data.ApplyURL)
			}

			// Checked before anything is yielded, so a tenant that ignores
			// "page" costs one wasted request rather than an endless stream of
			// duplicates.
			if pages.repeated(ids) {
				return
			}

			for _, item := range apiResp.Jobs {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				var (
					url         = strings.TrimSpace(strings.Replace(item.Data.ApplyURL, "http://", "https://", -1))
					titleStr    = strings.TrimSpace(item.Data.Title)
					locationStr = strings.TrimSpace(item.Data.FullLocation)
				)

				if url != "" && titleStr != "" && locationStr != "" {
					if !yield(&internal.JobPosting{
						Company:      company,
						URL:          url,
						Title:        titleStr,
						Location:     locationStr,
						Compensation: jibeCompensation(item.Data.SalaryMin, item.Data.SalaryMax, item.Data.SalaryCurrency, item.Data.SalaryFrequency),
						Source:       internal.PostingSource{Platform: jibePlatform, Key: company},
					}, nil) {
						return
					}
				}
			}

			// Counted in the units totalCount uses, postings the search matched,
			// not postings this adapter considered complete enough to yield.
			fetched += len(apiResp.Jobs)

			// totalCount is the only authority Jibe gives for "there is nothing
			// after this page", and it was decoded but never read until this
			// loop was bounded.
			//
			// It is trusted only when it exceeds a single page: a totalCount
			// equal to the page size is indistinguishable from a per-page count,
			// and reading one as the other would cap every large tenant at 100
			// postings, the silent-truncation failure this project has been
			// burned by before. Giving up that one case costs a single extra
			// request on a board whose posting count is an exact multiple of the
			// page size, which the short-page check below then ends.
			if apiResp.TotalCount > jibePageSize && fetched >= apiResp.TotalCount {
				return
			}

			if len(apiResp.Jobs) < jibePageSize {
				return
			}

			page++
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Jibe for company %q: the board was still serving full pages after %d pages of %d", company, jibeMaxPages, jibePageSize))
	}
}
