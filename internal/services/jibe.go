package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// jibePlatform is the ATS family this file registers, and the value that reaches
// [internal.PostingSource.Platform].
const jibePlatform = "jibe"

func init() {
	registerBuiltin(jibePlatform, multiJobsFuncNamed(Jibe, JibeCompanies, jibeCompanyName))
}

const (
	// jibePageSize is the number of postings requested per page.
	jibePageSize = 100

	// jibeMaxPages bounds how many pages a single Jibe tenant may be asked for.
	//
	// "totalCount" and [pageRepeatGuard] are the real stops; this is the backstop
	// for a tenant that reports no usable total and keeps serving different full
	// pages forever, which is how the unbounded loop this replaces ran up 5,001
	// requests and 500,001 duplicate postings against a stub in 0.8s. At 100
	// postings per page it still allows 200,000 postings from one company, well
	// above the largest board measured here (careers.dollargeneral.com, 88,854
	// on 2026-07-28), so reaching it means the board is misbehaving and the
	// adapter says so rather than crawling on.
	jibeMaxPages = 2000

	// jibeVanityPageFetchers bounds how many page requests are in flight for one
	// employer-hosted board at a time.
	//
	// It applies ONLY to vanity hosts. httpx.servicePolicyFor groups every
	// *.jibeapply.com tenant under one "jibeapply.com" limiter key, so fanning
	// out there would not send more requests: it would park more goroutines on a
	// semaphore the whole platform already shares. An employer's own careers
	// host falls through to the generic exact-host policy instead and therefore
	// owns its limiter key, which is what makes concurrency there a real win.
	//
	// Deliberately equal to httpx's default per-service limit, for the reason
	// [workdayPageFetchers] spells out: the limiter is the politeness ceiling,
	// not this constant.
	jibeVanityPageFetchers = 4
)

// JibeCompanies are the Jibe boards this project crawls, in two forms.
//
// A bare slug is a *.jibeapply.com tenant. A key containing a dot is an
// employer's own careers hostname: iCIMS rebuilt its modern career sites on Jibe
// and serves the identical /api/jobs endpoint from the EMPLOYER's domain, so
// careers.costco.com and jobs.jcp.com are Jibe boards that an adapter building
// only "{slug}.jibeapply.com" could never reach. The .icims.com host is not a
// substitute: it 404s on /api/jobs.
//
// Every entry here answered with real postings on 2026-07-28, and every one also
// appears in testdata/candidates/jibe_vanity_hosts.txt (hosts only; bare slugs
// predate that file). What is deliberately NOT here is any host that serves a
// board already reachable through another entry. One iCIMS tenant can be
// published under many employer domains, and the API has no per-brand filter, so
// each alias re-downloads the same requisitions under a different name: measured
// live, careers.orkin.com, careers.rollins.com, careers.westernpest.com and ten
// more all answer with the same 1,231 postings and the same
// orkin-careers-rollins.icims.com apply URLs. Registering all thirteen cost 156
// page requests per crawl to fetch 13 pages of distinct work, and put "Western
// Pest has 1,231 openings" into the output. The aliases stay staged in the
// candidate file, annotated with the entry they duplicate.
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

	// Employer-hosted boards. Each was probed on 2026-07-28 and answered with
	// postings; the count in the candidate file is that board's live totalCount.
	"aus.jibeapply.com",
	"careers.aarp.org",
	"careers.accentcare.com",
	"careers.adisseo.com",
	"careers.akima.com",
	"careers.amd.com",
	"careers.amica.ca",
	"careers.appliedmedical.com",
	"careers.arcfield.com",
	"careers.avalara.com",
	"careers.avispl.com",
	"careers.axa.com",
	"careers.beazley.com",
	"careers.bjsrestaurants.com",
	"careers.bowman.com",
	"careers.brasfieldgorrie.com",
	"careers.brightviewseniorliving.com",
	"careers.busybeeschildcare.co.uk",
	"careers.canyonranch.com",
	"careers.carlehealth.org",
	"careers.cbna.com",
	"careers.certara.com",
	"careers.chenega.com",
	"careers.chick-fil-a.com",
	"careers.cnb.com",
	"careers.confluencehealth.org",
	"careers.covenanthealthcare.com",
	"careers.crowley.com",
	"careers.docusign.com",
	"careers.doyon.com",
	"careers.emera.com",
	"careers.enlyte.com",
	"careers.fairview.org",
	"careers.farmandfleet.com",
	"careers.fluxys.com",
	"careers.flydubai.com",
	"careers.fm.com",
	"careers.foundationfinance.com",
	"careers.fultonbank.com",
	"careers.garmin.com",
	"careers.gateshudson.com",
	"careers.gilbaneco.com",
	"careers.gmr.net",
	"careers.gov2x.com",
	"careers.govcio.com",
	"careers.graitec.com",
	"careers.guard.com",
	"careers.haskell.com",
	"careers.haymarket.com",
	"careers.herzog.com",
	"careers.ice.com",
	"careers.incyte.com",
	"careers.infirmaryhealth.org",
	"careers.insightglobal.com",
	"careers.jhuapl.edu",
	"careers.kansascityymca.org",
	"careers.kehe.com",
	"careers.keo.com",
	"careers.kindermorgan.com",
	"careers.landrysinc.com",
	"careers.mastec.com",
	"careers.matthews.com",
	"careers.mcdean.com",
	"careers.medpace.com",
	"careers.msasafety.com",
	"careers.mymichigan.org",
	"careers.myrgroup.com",
	"careers.navistar.com",
	"careers.noblis.org",
	"careers.orkin.com",
	"careers.pacira.com",
	"careers.patelco.org",
	"careers.pnnl.gov",
	"careers.pplweb.com",
	"careers.primehealthcare.com",
	"careers.principal.com",
	"careers.promega.com",
	"careers.publicisgroupe.com",
	"careers.radnet.com",
	"careers.redeemerhealth.org",
	"careers.reliance.com",
	"careers.riversidehealthcare.org",
	"careers.rivian.com",
	"careers.royalfarms.com",
	"careers.rti.org",
	"careers.sammonsfinancialgroup.com",
	"careers.sarnova.com",
	"careers.sca.health",
	"careers.se.com",
	"careers.selectquote.com",
	"careers.shrinerschildrens.org",
	"careers.sightsavers.org",
	"careers.smh.com",
	"careers.smilebrands.com",
	"careers.softwareone.com",
	"careers.solusarc.co.uk",
	"careers.southglos.gov.uk",
	"careers.spiritaero.com",
	"careers.stlukes-stl.com",
	"careers.sunriseseniorliving.com",
	"careers.superiorambulance.com",
	"careers.teamues.com",
	"careers.thezenith.com",
	"careers.thompsonhospitality.com",
	"careers.tkcholdings.com",
	"careers.tompkinsfinancial.com",
	"careers.usa.skanska.com",
	"careers.usoncology.com",
	"careers.versiti.org",
	"careers.viasat.com",
	"careers.willowbridgepc.com",
	"chugach.jibeapply.com",
	"communitymedical.jibeapply.com",
	"conduent.jibeapply.com",
	"connection.jibeapply.com",
	"crashchampions.jibeapply.com",
	"danone.jibeapply.com",
	"designerbrands.jibeapply.com",
	"empowerai.jibeapply.com",
	"explore.enercon.com",
	"fedexfreight.jibeapply.com",
	"firstcitizens.jibeapply.com",
	"gdms.jibeapply.com",
	"gopilot.pilotcat.com",
	"gopuff.jibeapply.com",
	"greenstate.jibeapply.com",
	"harvard.jibeapply.com",
	"hazelden.jibeapply.com",
	"highgate.jibeapply.com",
	"jobportal.reyesholdings.com",
	"jobs.ajg.com",
	"jobs.aon.com",
	"jobs.ardenthealth.com",
	"jobs.bassett.org",
	"jobs.booking.com",
	"jobs.cumulusmedia.com",
	"jobs.firstwatch.com",
	"jobs.fraserhealth.ca",
	"jobs.geogroup.com",
	"jobs.keysight.com",
	"jobs.pdshealth.com",
	"jobs.postholdings.com",
	"jobs.selective.com",
	"jobs.smoothieking.com",
	"jobs.tdindustries.com",
	"jobs.trilogyhs.com",
	"jobs.tufts.edu",
	"jobs.ufhealth.org",
	"jobs.vnshealth.org",
	"jobs.ynhhs.org",
	"join.readingandmath.org",
	"karriere.korian.de",
	"lennox.jibeapply.com",
	"ltcrevolution.jibeapply.com",
	"marvin.jibeapply.com",
	"medicalsolutions.jibeapply.com",
	"nortonhealthcare.jibeapply.com",
	"osi-systems.jibeapply.com",
	"peraton.jibeapply.com",
	"pinkerton.jibeapply.com",
	"pittohio.jibeapply.com",
	"prideindustries.jibeapply.com",
	"regiscorp.jibeapply.com",
	"rentokil.jibeapply.com",
	"spa.jibeapply.com",
	"spglobal.jibeapply.com",
	"stifel.jibeapply.com",
	"talent.firsthealth.org",
	"talent.goldbelt.com",
	"talent.orrick.com",
	"uicalaska.jibeapply.com",
	"wellpath.jibeapply.com",
	"westerndental.jibeapply.com",
	"www.gbcijobs.com",
	"www.genesiscareers.jobs",
	"zenimax.jibeapply.com",
}

// jibeScalar is a value a Jibe tenant may send as a string or as a number.
//
// This file's oldest warning is that Jibe's payload is polymorphic across
// tenants and that a guessed Go *type* fails the decode and takes the whole
// tenant with it: modelling the top-level "meta_data" as a struct broke nine
// large employers at once, because some tenants send an object and others send
// a bare `false`. The fields this adapter reads have the same exposure —
// "req_id" is a string on every one of the 333 boards measured on 2026-07-28,
// but nothing in the API promises that, and one tenant quoting it as a number
// would otherwise cost that whole employer.
//
// Absorbing both spellings is the same defence [pinpointScalar] applies for the
// same reason. An object or an array is neither an id nor an amount, so it
// becomes empty rather than a field holding the literal text "{...}".
type jibeScalar string

// UnmarshalJSON implements [json.Unmarshaler].
func (s *jibeScalar) UnmarshalJSON(data []byte) error {
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

		*s = jibeScalar(strings.TrimSpace(text))

		return nil
	}

	if trimmed[0] == '{' || trimmed[0] == '[' {
		*s = ""

		return nil
	}

	*s = jibeScalar(trimmed)

	return nil
}

// text returns the scalar as a trimmed string.
func (s jibeScalar) text() string { return strings.TrimSpace(string(s)) }

// amount reads the scalar as a pay figure, reporting false when it is not a
// positive number.
func (s jibeScalar) amount() (float64, bool) {
	value, err := strconv.ParseFloat(s.text(), 64)
	if err != nil || value <= 0 {
		return 0, false
	}

	return value, true
}

// jibePosting is the subset of one Jibe search result this adapter uses.
//
// The rest of the response is still deliberately unmodelled. It carries Google
// Jobs derived info, facet lists, language counts and a per-posting "meta_data"
// object of iCIMS configuration, none of which a job seeker needs, and the
// top-level "meta_data" remains the polymorphic field that must never be given a
// fixed type.
//
// What HAS changed is that the fields below are no longer a guess. Every one of
// them was read off 29,230 live postings from 333 boards on 2026-07-28, with
// these presence rates:
//
//	posted_date       99.8%    req_id            99.5%
//	categories        96.7%    employment_type   84.5%
//	department         1.0%    salary_min_value   0.7%
//
// Two of those numbers correct claims this file used to make. It asserted that
// Jibe "publishes pay as structured numbers, and it populates them often,
// measured at 69 of 100 PetSmart postings"; PetSmart really does publish pay on
// 74 of its first 100, but PetSmart is 74 of the 205 postings that carry any pay
// at all across the whole platform, so "often" is true of one tenant and false
// of Jibe. And "currency is frequently empty even when the amounts are present"
// understates it: salary_currency was populated on 43 of those 205, and on the
// PetSmart page that produced the original measurement it is the empty string on
// all 74.
//
// "categories" is decoded but deliberately not mapped to
// [internal.JobPosting.Department], despite being the most widely populated of
// these. It is Jibe's job-family facet rather than an org unit, and its live
// values include "Retail Department Manager" and "Pet Grooming Salon Manager" —
// job titles. Filing those as a department would put wrong data into a field a
// filter cannot tell from right data. The narrower "department" is used instead,
// on the 1% of postings that have it.
type jibePosting struct {
	Title        string `json:"title"`
	ApplyURL     string `json:"apply_url"`
	FullLocation string `json:"full_location"`

	// Frequency is an enum like "HOURLY"; currency is usually empty even when
	// the amounts are present.
	SalaryMin       jibeScalar `json:"salary_min_value"`
	SalaryMax       jibeScalar `json:"salary_max_value"`
	SalaryCurrency  jibeScalar `json:"salary_currency"`
	SalaryFrequency jibeScalar `json:"salary_frequency"`

	// EmploymentType is the schema.org enum: FULL_TIME, PART_TIME, CONTRACTOR,
	// TEMPORARY, INTERN, VOLUNTEER. It is normalized rather than stored raw.
	EmploymentType jibeScalar `json:"employment_type"`

	Department jibeScalar `json:"department"`

	// ReqID is the employer's own requisition number. Slug is Jibe's identifier
	// for the posting within the tenant, and is the same string as ReqID on the
	// iCIMS-backed boards measured here — kept separate anyway because
	// [internal.JobPosting] documents them as different things, and a tenant
	// whose ATS is not iCIMS (petsmart's feed is Cadient) is free to differ.
	ReqID jibeScalar `json:"req_id"`
	Slug  jibeScalar `json:"slug"`

	PostedDate jibeScalar `json:"posted_date"`
	UpdateDate jibeScalar `json:"update_date"`
}

// jibeJobs is one page of a Jibe board's search response.
type jibeJobs struct {
	Jobs []struct {
		Data jibePosting `json:"data,omitempty"`
	} `json:"jobs"`

	TotalCount int `json:"totalCount"`
}

// jibeDateLayouts are the timestamp spellings accepted for posted_date and
// update_date.
//
// The live spelling is "2026-05-21T12:14:00+0000", which [time.RFC3339] does NOT
// parse: RFC 3339 requires a colon in the zone offset. That one character is the
// difference between a posting date on 99.8% of the platform and a posting date
// on none of it, and it is exactly the kind of claim no document could have
// settled — all 906 timestamps sampled across four boards had this shape. The
// colon-bearing and zoneless forms follow in case a tenant differs.
//
// Only unambiguous layouts, for the reason [oracleCloudDateLayouts] spells out:
// a slash-separated date is a different day in the US and in Europe.
var jibeDateLayouts = []string{
	"2006-01-02T15:04:05-0700",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// jibeTimestamp converts one of Jibe's dates to UTC, reporting false when it is
// missing or in a spelling this does not know.
func jibeTimestamp(raw jibeScalar) (time.Time, bool) {
	text := raw.text()
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range jibeDateLayouts {
		if stamp, err := time.Parse(layout, text); err == nil {
			return stamp.UTC(), true
		}
	}

	return time.Time{}, false
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

// jibeCompensation builds a pay range from a posting's salary fields, returning
// nil when the tenant published no amounts.
//
// Jibe sends explicit zeros rather than omitting the fields — on 99.3% of the
// platform, measured — so a zero range means "not disclosed" and must not become
// a posting that claims to pay nothing.
func jibeCompensation(posting jibePosting) *internal.Compensation {
	minValue, hasMin := posting.SalaryMin.amount()
	maxValue, hasMax := posting.SalaryMax.amount()

	if !hasMin && !hasMax {
		return nil
	}

	return &internal.Compensation{
		Min:        minValue,
		Max:        maxValue,
		Currency:   strings.ToUpper(posting.SalaryCurrency.text()),
		Period:     jibeFrequencies[strings.ToUpper(posting.SalaryFrequency.text())],
		Provenance: internal.ProvenanceEmployer,
	}
}

// jibeApplyURL returns a posting's link, resolved against the board it came from
// when the board published a relative one.
//
// "apply_url" is usually absolute, and usually points at a different system:
// iCIMS for most tenants, but also Workday, BrassRing, Taleo, Oracle Cloud and
// ADP. That is the platform's shape and this adapter has no alternative, since
// the search payload carries no link of its own — "slug" is the requisition
// number, not a path.
//
// A minority are root-relative. Measured on 2026-07-28 across a 685,000-posting
// crawl, 4,249 postings — every one of them FedEx — carried "apply_url" as
// "/freight-apply/apply/POSTING-3-958978" with no scheme or host. Those were
// stored verbatim, so this project published 4,249 postings whose URL cannot be
// opened at all, breaking the contract that every posting carries a link a
// person can follow. Nothing noticed, because a relative path is neither empty
// nor a duplicate: it passes the guard below and is unique in [internal.Dedupe].
//
// The board's own host is the right base, verified live: the FedEx path above
// answers 200 at https://fedex.jibeapply.com and 404 at careers.fedex.com.
func jibeApplyURL(key, applyURL string) string {
	link := strings.TrimSpace(strings.ReplaceAll(applyURL, "http://", "https://"))

	if strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//") {
		return "https://" + jibeHost(key) + link
	}

	return link
}

// jibePosting converts one decoded search result into a posting, reporting false
// when the board left out something a job seeker needs.
func jibeJobPosting(company, key string, item jibePosting) (*internal.JobPosting, bool) {
	var (
		link     = jibeApplyURL(key, item.ApplyURL)
		title    = strings.TrimSpace(item.Title)
		location = strings.TrimSpace(item.FullLocation)
	)

	if link == "" || title == "" || location == "" {
		return nil, false
	}

	posting := &internal.JobPosting{
		Company:  company,
		URL:      link,
		Title:    title,
		Location: location,

		Compensation:  jibeCompensation(item),
		Department:    item.Department.text(),
		RequisitionID: item.ReqID.text(),
		ExternalID:    item.Slug.text(),

		Source: internal.PostingSource{Platform: jibePlatform, Key: key},
	}

	if employment, ok := internal.NormalizeEmploymentType(item.EmploymentType.text()); ok {
		posting.EmploymentType = employment
	}

	if posted, ok := jibeTimestamp(item.PostedDate); ok {
		posting.PostedAt = posted
	}

	if updated, ok := jibeTimestamp(item.UpdateDate); ok {
		posting.UpdatedAt = updated
	}

	return posting, true
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

// jibeHostIsolated reports whether a key's board has a hostname to itself, and
// therefore a limiter key of its own in httpx.
//
// This is the whole test for whether per-source page fan-out is worth anything.
// httpx.servicePolicyFor collapses every *.jibeapply.com tenant onto one
// "jibeapply.com" key, so extra in-flight pages there contend for the same
// four-slot semaphore as the rest of the platform and buy nothing. An employer's
// own careers host gets the generic exact-host policy, so its four slots are its
// own. Note that a key may contain a dot and still be shared: "aus.jibeapply.com"
// is a registered board on the common backend.
func jibeHostIsolated(key string) bool {
	return !strings.HasSuffix(jibeHost(key), ".jibeapply.com")
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

	// A tenant on the shared platform host is named by its FIRST label:
	// "aus.jibeapply.com" is the tenant "aus", not the company "jibeapply".
	// Employer-owned vanity hosts below are the opposite case, which is why this
	// is checked first rather than folded into the logic underneath.
	if rest, ok := strings.CutSuffix(host, ".jibeapply.com"); ok && rest != "" {
		if idx := strings.Index(rest, "."); idx > 0 {
			return rest[:idx]
		}

		return rest
	}

	for _, prefix := range []string{"careers.", "career.", "jobs.", "job.", "www.", "apply.", "talent."} {
		if after, ok := strings.CutPrefix(host, prefix); ok {
			host = after

			break
		}
	}

	// Keep the registrable label. Taking the first label instead was wrong for
	// any careers subdomain the list above does not name: "join.readingandmath.org"
	// became the company "join", which both hid Reading & Math from
	// `--company readingandmath` and collided with the unrelated Oracle Cloud
	// employer genuinely called "join". The prefix list cannot be completed by
	// guessing; counting labels from the right does not need it to be.
	labels := strings.Split(host, ".")

	// Two-label public suffixes are the reason this is not simply labels[len-2]:
	// "busybeeschildcare.co.uk" would become "co". This project does not vendor a
	// public-suffix list, so the handful of suffixes its sources actually use are
	// named here, and anything unrecognised falls back to the two-label rule.
	if len(labels) >= 3 {
		switch strings.Join(labels[len(labels)-2:], ".") {
		case "co.uk", "org.uk", "ac.uk", "co.jp", "co.nz", "co.za",
			"co.in", "com.au", "com.br", "com.mx", "com.sg":
			return labels[len(labels)-3]
		}
	}

	if len(labels) >= 2 {
		return labels[len(labels)-2]
	}

	return host
}

// jibePlan is what a board's first page says about the rest of its pages.
type jibePlan struct {
	// pages are the page numbers still to fetch, in ascending order. Empty means
	// the first page was the whole board.
	pages []int

	// known reports whether the board gave a usable total. When false the caller
	// must page sequentially until a short or repeated page, because there is no
	// authority for where the board ends.
	known bool
}

// jibePagesAfter reads a board's reported total into a plan for the pages after
// the first.
//
// totalCount is trusted only when it exceeds a single page: a totalCount equal
// to the page size is indistinguishable from a per-page count, and reading one
// as the other would cap every large tenant at 100 postings, the silent
// truncation this project has been burned by before. Giving up that one case
// costs a single extra request on a board whose posting count is an exact
// multiple of the page size, which the short-page check then ends.
func jibePagesAfter(total, served int) jibePlan {
	if total <= jibePageSize || served <= 0 || total <= served {
		return jibePlan{known: total > jibePageSize}
	}

	// Bounded by jibeMaxPages counting the page already fetched, so a board
	// reporting an absurd total cannot schedule unbounded work.
	last := (total + jibePageSize - 1) / jibePageSize
	last = min(last, jibeMaxPages)

	pages := make([]int, 0, last-1)
	for page := 2; page <= last; page++ {
		pages = append(pages, page)
	}

	return jibePlan{pages: pages, known: true}
}

// Jibe returns job postings from Jibe's API for a given company. It's unclear to me where this
// API is documented now, but it seems like it's still available even after the ICIIMS acquisition.
//
// https://www.icims.com/company/newsroom/icims-acquires-jibe-to-provide-employers-best-in-class-candidate-engagement-and-recruitment-marketing-capabilities/
//
// Pagination takes one of two shapes, chosen by [jibeHostIsolated]. An employer
// -hosted board owns its limiter key, so once the first page has reported
// totalCount every remaining page number is known and they are fetched with
// bounded concurrency ([jibeVanityPageFetchers]), postings yielded as they
// arrive rather than in page order — ordering within one employer was never
// meaningful, and waiting for page N-1 before emitting page N is what makes a
// large board take as many round trips as it has pages. That matters here:
// careers.dollargeneral.com answered with 88,854 postings on 2026-07-28, which is
// 889 strictly sequential requests today.
//
// A *.jibeapply.com board pages sequentially instead, because every tenant on
// that host shares one limiter key and concurrency there would only queue.
func Jibe(ctx context.Context, httpClient *http.Client, key string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			company = jibeCompanyName(key)
			baseURL = "https://" + jibeHost(key) + "/api/jobs"
			pages   pageRepeatGuard
		)

		// Cancelling this context is how a consumer that stops early, or a page
		// that fails, tells the in-flight fetchers to wind down at once. The
		// caller's context is kept separately so "the caller cancelled us" can be
		// told apart from "we cancelled ourselves on the way out".
		parentCtx := ctx

		ctx, cancel := context.WithCancel(parentCtx)
		defer cancel()

		// emit hands one page's postings to the consumer, reporting whether
		// iteration should continue. A false result means either the consumer
		// asked to stop or the caller's context was cancelled, and in the latter
		// case the error has already been yielded.
		emit := func(page *jibeJobs) bool {
			for _, item := range page.Jobs {
				if err := parentCtx.Err(); err != nil {
					yield(nil, err)

					return false
				}

				posting, ok := jibeJobPosting(company, key, item.Data)
				if !ok {
					continue
				}

				if !yield(posting, nil) {
					return false
				}
			}

			return true
		}

		// repeated fingerprints a page so a board that ignores "page" costs one
		// wasted request rather than an endless stream of duplicates. It is
		// checked before anything from the page is yielded.
		repeated := func(page *jibeJobs) bool {
			ids := make([]string, 0, len(page.Jobs))
			for _, item := range page.Jobs {
				ids = append(ids, item.Data.ApplyURL)
			}

			return pages.repeated(ids)
		}

		first, err := jibePage(ctx, httpClient, key, baseURL, 1)
		if err != nil {
			yield(nil, err)

			return
		}

		if len(first.Jobs) == 0 || repeated(first) || !emit(first) {
			return
		}

		// A short first page is the end of the board, whatever totalCount says.
		// This has been the adapter's behaviour since it was written and is kept
		// deliberately: totalCount counts what the *search* matched, and several
		// boards report a figure larger than the pages they will actually serve.
		// Trusting the number over the page here would fan out dozens of
		// requests for pages the board has already told us do not exist.
		if len(first.Jobs) < jibePageSize {
			return
		}

		plan := jibePagesAfter(first.TotalCount, len(first.Jobs))

		var live bool

		switch {
		case plan.known && len(plan.pages) == 0:
			// The board's own total says the first page was all of it.
			live = true
		case plan.known && jibeHostIsolated(key):
			live = jibeFanOut(ctx, cancel, httpClient, key, baseURL, plan.pages, repeated, emit, yield)
		default:
			live = jibeSequential(ctx, httpClient, key, baseURL, len(first.Jobs), first.TotalCount, repeated, emit, yield)
		}

		// A board cut short by the caller's cancellation returned partial
		// results. Say so, rather than let a truncated employer look complete.
		//
		// Gated on live, which reports that nothing has already ended this
		// iterator. Without it a consumer that stopped early during a cancelled
		// crawl would be yielded to after returning false, which panics, and a
		// cancellation that emit had already reported would be reported twice.
		if err := parentCtx.Err(); live && err != nil {
			yield(nil, err)
		}
	}
}

// jibeSequential pages a board one request at a time, starting from page two.
//
// This is the path for every *.jibeapply.com tenant, and for any board that
// reported no usable total. It stops on the board's own total, on a short page,
// on a repeated page, and finally on [jibeMaxPages].
//
// It reports whether the iterator is still live: false once an error has been
// yielded or the consumer has stopped, so the caller does not yield again.
func jibeSequential(
	ctx context.Context,
	httpClient *http.Client,
	key, baseURL string,
	fetched, total int,
	repeated func(*jibeJobs) bool,
	emit func(*jibeJobs) bool,
	yield func(*internal.JobPosting, error) bool,
) bool {
	// fetched is counted in the units totalCount uses: postings the search
	// matched, not postings this adapter considered complete enough to yield.
	if total > jibePageSize && fetched >= total {
		return true
	}

	for page := 2; page <= jibeMaxPages; page++ {
		next, err := jibePage(ctx, httpClient, key, baseURL, page)
		if err != nil {
			yield(nil, err)

			return false
		}

		if len(next.Jobs) == 0 || repeated(next) {
			return true
		}

		if !emit(next) {
			return false
		}

		fetched += len(next.Jobs)

		if next.TotalCount > jibePageSize && fetched >= next.TotalCount {
			return true
		}

		if len(next.Jobs) < jibePageSize {
			return true
		}
	}

	yield(nil, fmt.Errorf("refusing to keep paginating Jibe for company %q: the board was still serving full pages after %d pages of %d", key, jibeMaxPages, jibePageSize))

	return false
}

// jibeFanOut fetches the given page numbers with bounded concurrency, handing
// each page to emit as it arrives.
//
// Structured after [Workday]'s fan-out, which is the pattern this project
// already trusts: a slot is held until the result has been handed over, so
// prefetching runs at most [jibeVanityPageFetchers] pages ahead of the consumer
// instead of buffering a whole board in memory, and the iterator does not return
// until every fetcher has exited.
//
// It reports whether the iterator is still live, on the same contract as
// [jibeSequential].
func jibeFanOut(
	ctx context.Context,
	cancel context.CancelFunc,
	httpClient *http.Client,
	key, baseURL string,
	pages []int,
	repeated func(*jibeJobs) bool,
	emit func(*jibeJobs) bool,
	yield func(*internal.JobPosting, error) bool,
) bool {
	type pageResult struct {
		doc *jibeJobs
		err error
	}

	var (
		results   = make(chan pageResult)
		sem       = make(chan struct{}, jibeVanityPageFetchers)
		exhausted = make(chan struct{})
		exhaust   sync.Once
		wg        sync.WaitGroup
	)

	// stopScheduling is called when a page comes back with no postings at all,
	// which means the board's reported total overshot what it will serve; the
	// pages past that point would only fetch more empty ones.
	//
	// A short but non-empty page is deliberately not treated this way. It is
	// indistinguishable from a board hiccuping mid-crawl, and acting on it would
	// silently truncate an employer.
	stopScheduling := func() { exhaust.Do(func() { close(exhausted) }) }

	go func() {
		// results must not be closed until every sender has finished, or a
		// straggling send would panic.
		defer func() {
			wg.Wait()
			close(results)
		}()

		for _, page := range pages {
			// Checked before the selects below because a select with two ready
			// cases picks at random, which would let a new request start after
			// cancellation.
			if ctx.Err() != nil {
				return
			}

			// Waiting for a slot is where this loop spends nearly all of its
			// time, so the stop signals have to be selected on here too, not
			// only polled between iterations.
			select {
			case sem <- struct{}{}:
			case <-exhausted:
				return
			case <-ctx.Done():
				return
			}

			// A slot may have been ready at the same moment as a stop signal,
			// and select picks at random between ready cases.
			select {
			case <-exhausted:
				<-sem

				return
			default:
			}

			wg.Add(1)

			go func(page int) {
				defer wg.Done()
				defer func() { <-sem }()

				doc, err := jibePage(ctx, httpClient, key, baseURL, page)

				select {
				case results <- pageResult{doc: doc, err: err}:
				case <-ctx.Done():
				}
			}(page)
		}
	}()

	// stop unwinds the fan-out before returning: cancel, then drain until
	// results is closed, which only happens once every fetcher has exited.
	// Returning without draining would leave goroutines running past the end of
	// the iterator.
	stop := func() {
		cancel()

		for range results { //nolint:revive // draining is the point
		}
	}

	for result := range results {
		if result.err != nil {
			stop()
			yield(nil, result.err)

			return false
		}

		if len(result.doc.Jobs) == 0 {
			stopScheduling()

			continue
		}

		// Pages arrive out of order, so the guard is a set of fingerprints
		// rather than a comparison with the previous page: a board that ignores
		// "page" answers every request with page one, and each repeat is caught
		// wherever it lands.
		if repeated(result.doc) {
			continue
		}

		if !emit(result.doc) {
			stop()

			return false
		}
	}

	return true
}
