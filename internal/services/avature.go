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

// avaturePlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const avaturePlatform = "avature"

func init() {
	registerBuiltin(avaturePlatform, multiJobsFuncNamed(Avature, AvatureCareerSites, avatureCompanyName))

	// Every career site below is one vendor's backend, whether it answers on
	// {tenant}.avature.net or on the employer's own hostname: 59 of the 87
	// probed redirect a *.avature.net request onto a vanity host
	// (jobs.ea.com, careers.lululemon.com, jobs.justice.gov.uk), and those
	// vanity hosts are CNAME fronts onto the same service. Left on the generic
	// exact-host policy this platform alone would put 348 concurrent requests
	// on one backend, which is the shape that rate-limited 56 Workable boards
	// into looking dead.
	//
	// A `.avature.net` suffix arm in httpx.servicePolicyFor would NOT be
	// enough here, precisely because two thirds of these hosts are not under
	// that domain. Registering the host list from this file is what covers
	// them; the wanted httpx change is reported rather than made here.
	httpx.RegisterSharedBackend(avaturePlatform, avatureHosts()...)
}

const (
	// avatureMaxPages bounds a single career site's walk, unconditionally.
	//
	// The signals this adapter actually ends on are the board's own pagination
	// and an empty page; this is the backstop, because this codebase has already
	// paid for an offset loop that trusted a board and ran 5,001 times.
	//
	// 400 is roughly 2.7 times the deepest registered walk measured on
	// 2026-07-29 (careers.tesco.com, 1,463 postings in 148 requests) and covers
	// 2,400 postings on a six-per-page site or 10,000 on a 25-per-page one. It
	// is a bound on requests rather than on postings because Avature's page size
	// is a per-tenant setting, measured at 2, 5, 6, 10, 12, 15, 18, 19, 20 and
	// 25 across the 87 sites probed. In practice [avatureResultWindow] binds
	// first on every site large enough to reach either.
	avatureMaxPages = 400

	// avatureJobPath is the path segment every posting link on a career site
	// carries: {section}/JobDetail/{title-slug}/{id}.
	avatureJobPath = "/JobDetail/"

	// avatureResultWindow is the deepest record offset an Avature career site
	// will page to. Past it the platform stops paginating and starts lying.
	//
	// Measured on 2026-07-29. nva.avature.net pages ten at a time and answered
	// offset 2000 with ten postings and offset 2010 with a 333 KB "Oops...
	// Something went wrong" page -- HTTP 200, no postings, no pagination.
	// manpowergroupco.avature.net pages six at a time and did the same thing at
	// 2004, in Spanish. Both cut at the first offset strictly greater than
	// 2000, so the window is a record count and not a page count.
	//
	// This is the single most dangerous fact about the platform, because the
	// truncated walk looks exactly like a completed one: a career site with
	// 5,000 openings quietly reports the 2,010 it could reach, on every crawl,
	// with no error. Four of the 87 sites probed are already past it. The walk
	// therefore reports an error rather than a total when it is cut off here;
	// see [Avature].
	avatureResultWindow = 2000
)

// AvatureCareerSites holds the Avature career sites this project crawls, one
// "slug,host,section" triple per entry.
//
// # The key is a triple because Avature tenancy is a triple
//
// docs/research/ats-platform-survey.md flags Avature as one of five platforms
// with "non-guessable composite tenancy", and understates it. The section is
// per tenant and not derivable -- the sites below use /careers, /en_US/careers,
// /en_GB/careers, /fr_CA/careers, /es_ES/careers, /de_DE/careers, /jobs and
// /campuscareers -- and so, it turns out, is the host.
//
// # 59 of 87 sites do not answer on the host the candidate file lists
//
// testdata/candidates/avature_tenants.txt records 87 triples of the form
// {tenant}.avature.net{section}, all of which answered HTTP 200 when it was
// staged. Probed again on 2026-07-29, 59 of the 87 answered a redirect: 26 onto
// the employer's own hostname (ea.avature.net -> jobs.ea.com,
// justicejobs.avature.net -> jobs.justice.gov.uk, colorado.avature.net ->
// jobs.colorado.edu) and 33 onto a locale-prefixed section on the same host
// (koch.avature.net/careers -> koch.avature.net/en_US/careers).
//
// That matters for more than tidiness. Every posting link on a redirected site
// is written with the destination host, so a walk keyed on the pre-redirect
// host yields URLs on a host it was not asked to crawl. Registering the
// destination is what keeps [avatureCanonicalURL]'s same-host check meaningful,
// and that check is the one thing standing between this platform and the
// double-count class of bug that cost this repo 5,103 duplicate Lowe's
// postings. The triples below are therefore the post-redirect ones, and each
// was re-probed at its destination.
//
// # This list is measured, not staged
//
// All 87 were walked to their last page on 2026-07-29, 3,352 HTTP requests for
// 27,747 distinct posting URLs. The 73 registered below are what survived, and
// each carries its own measured size and cost:
//
//	73 sites, 2,141 HTTP requests, 18,028 distinct posting URLs
//	= 8.4 postings per request
//
// That is the number to schedule this platform by, and it is worse than every
// JSON lane in this binary: docs/research/ats-platform-survey.md ranks
// SuccessFactors at ~144 postings per request and Oracle ORC at ~72, and puts
// Avature among the platforms that "will blow the time budget". At 8.5 it sits
// between ADP (~8) and Personio (~10) on that ranking, so the survey's verdict
// is right in direction and the cost is now a measurement rather than an
// estimate. 18,028 postings for 2,141 requests is still a better trade than
// most of what is left unregistered.
//
// # The 14 sites deliberately left out
//
// Four are larger than Avature's paging window and cannot be crawled to the end
// at all; see [avatureResultWindow]. Registering them would publish a fraction
// of each employer with no sign that anything was missing:
//
//	dpdhlgroup       2,010 of an unknown total (DHL Group)
//	nva              2,010 of an unknown total (NVA pet hospitals)
//	manpowergroupco  2,004 of an unknown total (ManpowerGroup Colombia)
//	koch             2,004 of an unknown total (Koch)
//
// Seven are the vendor's own sandbox and UAT instances, whose postings are test
// artefacts rather than jobs. docs/adding-a-source.md is explicit that a board
// like this is not a real board, and the titles settle it: sandboxea publishes
// "Test", "Aires Job Test"; sandboxamspsr's single posting is "Non Civil Test";
// sandboxpomerleau publishes "TEST CAMPUS"; sandboxbnc publishes "Redirigé" and
// "BC 2 langues"; uatashfieldhealthcare leads with "!Show this in portal".
// Between them they would have added 1,273 postings that do not exist:
// sandboxamspsr, sandboxashfieldhealthcare, sandboxbnc, sandboxea, sandboxino1,
// sandboxpomerleau, uatashfieldhealthcare.
//
// Two are employers this project already crawls on another platform under the
// same display name, and double_count_test.go requires a measured verdict
// before an overlap may be registered. Both walks were run so the verdict can
// be recorded by whoever owns that file, and neither is registered here:
//
//	                       avature          jibe             shared URLs  shared titles
//	skanska                130 (120 t)      219 (118 t)      0            4 of 118
//	stjude                 167 (152 t)      163 (148 t)      0            32 of 148
//
// Both were crawled through both adapters on 2026-07-29 and neither shares a
// single posting URL, which is expected: the URL differs by route, which is
// exactly why [internal.Dedupe] cannot collapse these and why the verdict has
// to be measured rather than assumed. On titles they are two different shapes.
// Skanska's Avature site is the UK business (Visitor Centre Administrator,
// Building Services, Middlesex) and its Jibe board is careers.usa.skanska.com,
// so 4 shared titles out of 118 is the ordinary collision of generic job names
// between two disjoint boards. St. Jude is one board published twice: 32 of 148
// titles are shared and the two sizes are within 3% of each other.
//
// One is careers.lululemon.com, which failed two of the three attempts made on
// 2026-07-29: once mid-walk under concurrency and once on its very first
// request, with "stream error: stream ID 7; INTERNAL_ERROR; received from
// peer". The one attempt that finished read 1,106 postings in 112 requests at
// one request every 0.9s, so the board is real and readable; two failures in
// three is still a source that would spend most nights in the error column, and
// docs/adding-a-source.md would rather have it staged than flapping.
//
// # Three sites needed a second, slower walk
//
// justicejobs, lululemoninc and radpartners failed mid-walk on a first pass run
// at three concurrent requests across the whole platform, and completed cleanly
// at one request every 0.9s (403, 1,106 and 652 postings). koch answered HTTP
// 406 after 335 requests. That is one vendor backend refusing a burst rather
// than four broken boards, and it is the measurement behind the pacing this
// file's init asks for.
//
// # Every registered site was re-read through this adapter
//
// All 73 were walked again on 2026-07-29 through [Avature] itself rather than a
// probe script, and every one returned postings with no error and no posting
// rejected by internal/tests.CheckJobPosting.
var AvatureCareerSites = []string{
	"a2milkkf,a2milkkf.avature.net,/careers",                                             //     9 postings,    3 requests
	"advocateaurorahealth,clinicianjobs.advocatehealth.org,/careers",                     //   580 postings,   98 requests
	"aesc,jobs.aesc-group.com,/en_US/careers",                                            //    46 postings,    9 requests
	"ally,ally.avature.net,/careers",                                                     //    65 postings,   12 requests
	"amswh,amswh.avature.net,/careers",                                                   //    89 postings,   19 requests
	"astellas,astellas.avature.net,/en_GB/careers",                                       //    39 postings,    5 requests
	"astellasjapan,astellasjapan.avature.net,/en_GB/careers",                             //    56 postings,    7 requests
	"auspost,jobs.auspost.com.au,/en_GB/careers",                                         //   149 postings,   26 requests
	"baufest,baufest.avature.net,/jobs",                                                  //    31 postings,    3 requests
	"berenberg,berenberg.avature.net,/en_GB/careers",                                     //    35 postings,    5 requests
	"bloomberg,bloomberg.avature.net,/careers",                                           //   398 postings,   36 requests
	"bmcrecruit,jobs.bmc.com,/careers",                                                   //   115 postings,   21 requests
	"bnc,emplois.bnc.ca,/fr_CA/careers",                                                  //   346 postings,   19 requests
	"bravura,bravura.avature.net,/en_US/careers",                                         //     5 postings,    2 requests
	"broadinstitute,broadinstitute.avature.net,/en_US/careers",                           //    42 postings,    8 requests
	"cdcn,cdcn.avature.net,/careers",                                                     //   190 postings,    9 requests
	"cicor,cicor.avature.net,/en_US/careers",                                             //    16 postings,    4 requests
	"ciusss,ciusss.avature.net,/fr_CA/careers",                                           //   132 postings,   23 requests
	"colorado,jobs.colorado.edu,/jobs",                                                   //    69 postings,    4 requests
	"cyclecarriage,cyclecarriage.avature.net,/careers",                                   //    37 postings,    4 requests
	"deloittebe,deloittebe.avature.net,/en_US/careers",                                   //   241 postings,   15 requests
	"deloittece,apply.deloittece.com,/en_US/careers",                                     //   540 postings,   55 requests
	"deloittecm,deloittecm.avature.net,/en_US/careers",                                   //   441 postings,   75 requests
	"deloitteus,apply.deloitte.com,/en_US/careers",                                       //   962 postings,   98 requests
	"dfiretailgroup,dfiretailgroup.avature.net,/en_US/careers",                           //   133 postings,   15 requests
	"dhlconsulting,dhlconsulting.avature.net,/en_US/careers",                             //     6 postings,    2 requests
	"dth,dth.avature.net,/en_US/careers",                                                 //    56 postings,    7 requests
	"ea,jobs.ea.com,/en_US/careers",                                                      //   391 postings,   21 requests
	"fb,careers.fbcareers.com,/careers",                                                  //   223 postings,   39 requests
	"fmlogistic,fmlogistic.avature.net,/en_US/careers",                                   //     2 postings,    2 requests
	"fonterrakf,careers.fonterra.com,/careers",                                           //    40 postings,    8 requests
	"forvis,forvis.avature.net,/campuscareers",                                           //    70 postings,   13 requests
	"frequentis,jobs.frequentis.com,/careers",                                            //    74 postings,    5 requests
	"gpshospitality,gpshospitality.avature.net,/careers",                                 //  1170 postings,  118 requests
	"harmanglobal,jobsearch.harman.com,/en_US/careers",                                   //   438 postings,   23 requests
	"insperity,insperity.avature.net,/en_US/careers",                                     //   124 postings,   22 requests
	"intercaretherapy,intercaretherapy.avature.net,/en_US/careers",                       //   189 postings,   33 requests
	"jackhenry,jackhenry.avature.net,/careers",                                           //    54 postings,   10 requests
	"jakala,careers.jakala.com,/en_US/careers",                                           //    61 postings,   12 requests
	"jrg,jrg.avature.net,/en_US/careers",                                                 //    49 postings,   10 requests
	"justicejobs,jobs.justice.gov.uk,/careers",                                           //   403 postings,   69 requests
	"laplanduk,laplanduk.avature.net,/en_US/careers",                                     //    81 postings,   15 requests
	"lindner,lindner.avature.net,/de_DE/careers",                                         //    36 postings,    5 requests
	"lol,lol.avature.net,/careers",                                                       //    11 postings,    3 requests
	"mantech,careers.mantech.com,/en_US/careers",                                         //   727 postings,  123 requests
	"mclaren,mclarencareers.mclaren.com,/careers",                                        //    85 postings,    6 requests
	"mercadona,mercadona.avature.net,/es_ES/careers",                                     //   392 postings,   67 requests
	"mhcta,mhcta.avature.net,/careers",                                                   //   282 postings,   20 requests
	"mt,careers.mt.com,/en_US/careers",                                                   //   487 postings,   50 requests
	"onecall,onecall.avature.net,/careers",                                               //    19 postings,    5 requests
	"pepsicoglobalpontoon,pepsicoglobalpontoon.avature.net,/careers",                     //   656 postings,   34 requests
	"pomerleau,pomerleau.avature.net,/en_US/careers",                                     //   231 postings,   40 requests
	"primero,primero.avature.net,/en_GB/careers",                                         //    77 postings,    9 requests
	"radpartners,radpartners.avature.net,/careers",                                       //   652 postings,  110 requests
	"regis,regis.avature.net,/en_US/careers",                                             //   163 postings,   29 requests
	"resourcebank,resourcebank.avature.net,/careers",                                     //    24 postings,    4 requests
	"rohdeschwarz,jobs.rohde-schwarz.com,/en_US/careers",                                 //   235 postings,   41 requests
	"santos,recruitment.santos.com,/careers",                                             //     2 postings,    2 requests
	"synopsys,synopsys.avature.net,/careers",                                             //   405 postings,   69 requests
	"tesco,careers.tesco.com,/en_GB/careers",                                             //  1463 postings,  148 requests
	"tescoinsuranceandmoneyservices,tescoinsuranceandmoneyservices.avature.net,/careers", //     6 postings,    2 requests
	"totalenergies,jobs.totalenergies.com,/en_US/careers",                                //  1045 postings,   54 requests
	"traderjoes,traderjoes.avature.net,/careers",                                         //   220 postings,   12 requests
	"transcom,apply.careers.transcom.com,/en_US/careers",                                 //   199 postings,   35 requests
	"travisperkins,travisperkins.avature.net,/careers",                                   //   248 postings,   14 requests
	"uclahealth,uclahealth.avature.net,/careers",                                         //   675 postings,  115 requests
	"unifi,careers.unifiservice.com,/careers",                                            //   704 postings,   37 requests
	"uop,uop.avature.net,/careers",                                                       //    11 postings,    4 requests
	"vanoord,vanoord.avature.net,/en_US/careers",                                         //    51 postings,   10 requests
	"wickes,wickes.avature.net,/careers",                                                 //   125 postings,    8 requests
	"workmyway,emea.workmyway.com,/en_GB/careers",                                        //   233 postings,   25 requests
	"xerox,xerox.avature.net,/en_US/careers",                                             //   358 postings,   73 requests
	"zungfu,zungfu.avature.net,/en_US/careers",                                           //     9 postings,    3 requests
}

// avatureHosts returns the distinct hostname of every registered career site,
// for the shared-backend registration in this file's init.
func avatureHosts() []string {
	seen := make(map[string]bool, len(AvatureCareerSites))

	hosts := make([]string, 0, len(AvatureCareerSites))

	for _, site := range AvatureCareerSites {
		_, host, _, ok := avatureSite(site)
		if !ok || seen[host] {
			continue
		}

		seen[host] = true
		hosts = append(hosts, host)
	}

	return hosts
}

// avatureSite splits a "slug,host,section" registry key.
//
// The section is kept exactly as written, including its leading slash and
// including the empty string, because it is a path prefix rather than a name.
func avatureSite(key string) (slug, host, section string, ok bool) {
	fields := strings.Split(strings.TrimSpace(key), ",")
	if len(fields) != 3 {
		return "", "", "", false
	}

	slug = strings.TrimSpace(fields[0])
	host = strings.ToLower(strings.TrimSpace(fields[1]))
	section = strings.TrimSpace(fields[2])

	if slug == "" || host == "" {
		return "", "", "", false
	}

	return slug, host, section, true
}

// avatureCompanyName is the display name for a career site, which is the first
// field of its key.
//
// The key is a triple, so without this the user-facing company list would carry
// "ally,ally.avature.net,/careers" and `--company ally` would still match it
// only by accident of substring. [SourcesMatching] matches the key too, so
// `--company jobs.ea.com` also selects the right source.
func avatureCompanyName(key string) string {
	slug, _, _, ok := avatureSite(key)
	if !ok {
		return key
	}

	return slug
}

// avatureSearchURL is a career site's job list at one record offset.
//
// jobOffset is a record offset rather than a page number, which is worth saying
// out loud because it is the parameter this adapter has to get right: a
// past-the-end offset does not error, it answers HTTP 200 with a "No jobs found"
// page that still advertises a Next link pointing back at offset 6. See
// [avatureNextOffset].
func avatureSearchURL(host, section string, offset int) string {
	return "https://" + host + section + "/SearchJobs/?jobOffset=" + strconv.Itoa(offset)
}

// avatureCanonicalURL turns a link on a search page into the career site's own
// posting URL, reporting the host it actually points at when that is not this
// site.
//
// Three things are checked and all three are load-bearing:
//
//   - the scheme, which drops the "mailto:?body=https://.../JobDetail/..."
//     share links every result row on the article skin carries. Counting those
//     is what inflated the candidate file's per-page numbers by up to 5x
//     (xerox: 25 "JobDetail links" on a page holding 5 postings);
//   - the host, because yielding a URL on another host is the single mistake
//     that caused every double count found in this repo. The rejected host is
//     returned rather than discarded so the caller can say which site it is,
//     which is how a career site that has moved shows up as an error instead of
//     as an employer who stopped hiring;
//   - the trailing id, which must be digits. /JobDetail/ also appears in
//     query strings ("...?returnUrl=%2Fcareers%2FJobDetail%2F...") and those do
//     not survive the path check, but a percent-decoded one would.
//
// The query string is dropped. Synopsys writes every result link with a
// ?businessTitle= parameter holding the title again; leaving it on would make
// the same opening a different URL to [internal.Dedupe] than the one the
// board's own share link or a job seeker's address bar produces.
func avatureCanonicalURL(host, href string) (canonical, otherHost string, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", "", false
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", false
	}

	if !strings.Contains(parsed.Path, avatureJobPath) {
		return "", "", false
	}

	id := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	if id == "" {
		return "", "", false
	}

	for _, r := range id {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}

	if linkHost := strings.ToLower(parsed.Hostname()); linkHost != "" && linkHost != host {
		return "", linkHost, false
	}

	return "https://" + host + parsed.Path, "", true
}

// avatureExternalID returns the numeric posting id from a canonical URL, which
// is the {id} in {section}/JobDetail/{title-slug}/{id}.
func avatureExternalID(canonical string) string {
	return canonical[strings.LastIndex(canonical, "/")+1:]
}

// avatureAnchors returns every anchor on a page that carries an href, in
// document order.
//
// Rows are not looked for by class, and that is deliberate. Five distinct list
// templates were measured across the 87 sites on 2026-07-29 and no class is
// common to them:
//
//	<article class="article article--result">          ally, xerox, synopsys, forvis, tesco
//	<article class="article article--result 1">        tesco, forvis (a trailing index in the class)
//	<tr class="card--box"><td><h3 class="article__header__text__title">
//	                                                   deloittebe (a table, no <article> at all)
//	<div class="jobItem"><p class="jobTitle">          lol (Land O'Lakes, a legacy skin)
//	<li class="listSingleColumnItem">                  colorado (jobs.colorado.edu)
//
// What every one of them does have is a link to {section}/JobDetail/{slug}/{id}
// on its own host, so that link is the row marker and [avatureRow] recovers the
// surrounding row from it rather than the other way round.
func avatureAnchors(root *html.Node) []*html.Node {
	return icimsFindAll(root, func(n *html.Node) bool {
		return n.Data == "a" && icimsAttr(n, "href") != ""
	})
}

// avatureRow returns the largest ancestor of a posting link that still contains
// only that one posting, which is the row the template drew for it.
//
// Climbing from the link is what makes this work on all five templates without
// naming any of them: whatever element the template used, the row is by
// definition the biggest box around the title that does not reach the next job.
// The link itself is returned when even its parent already holds two postings,
// which costs the row's metadata rather than the posting.
func avatureRow(host string, anchor *html.Node) *html.Node {
	row := anchor

	for parent := anchor.Parent; parent != nil; parent = parent.Parent {
		if avatureCountPostings(host, parent) != 1 {
			break
		}

		row = parent
	}

	return row
}

// avatureCountPostings reports how many distinct postings are linked under a
// node.
func avatureCountPostings(host string, n *html.Node) int {
	seen := make(map[string]bool, 2)

	for _, anchor := range avatureAnchors(n) {
		canonical, _, ok := avatureCanonicalURL(host, icimsAttr(anchor, "href"))
		if ok {
			seen[canonical] = true
		}
	}

	return len(seen)
}

// avatureFieldClasses maps the semantic span classes Avature's current template
// emits onto what they hold.
//
// These are read in preference to anything positional because they are
// unambiguous and because which fields a row carries is a per-tenant
// configuration: forvis publishes location, ref and posted date; synopsys
// publishes job id, hire type and posted date and no location at all; tesco
// publishes legal entity, location, contract type and an APPLY-BY date, which
// is emphatically not a posted date and is why the date is matched on its own
// class rather than on "a span holding something date-shaped".
var avatureFieldClasses = map[string]string{
	"list-item-location": "location",
	"locationText":       "location",
	"list-item-posted":   "posted",
	"daysAgo":            "posted",
	"list-item-ref":      "requisition",
	"list-item-jobId":    "requisition",
}

// avatureLabelledPrefixes maps the label an older template writes inline onto
// the same field names, for rows that carry no class at all.
//
// The ally skin writes an unclassed <span> per value and separates them with
// bare punctuation: "Charlotte , NC , USA , Ref #22821 . Posted Jul-28-2026".
// Only the last two are self-describing, so those two are matched here and the
// remaining unlabelled spans become the location, in the order the row wrote
// them.
var avatureLabelledPrefixes = map[string]string{
	"ref #":  "requisition",
	"job id": "requisition",
	"posted": "posted",
}

// avatureIgnoredSpanPrefixes are the labels an unclassed span can carry that are
// neither a location nor anything this project stores.
//
// "Apply by" earns its place: tesco writes a closing date in exactly the shape a
// posted date has, and treating it as one would publish a date in the future as
// the day the job appeared.
var avatureIgnoredSpanPrefixes = map[string]string{
	"apply by":     "",
	"closing date": "",
	"closes":       "",
}

// avatureSpans returns the metadata spans of a result row: every <span> under
// it that is not inside a link.
//
// Skipping links is what keeps the row's social-share block out of the data. On
// the ally skin every row ends with anchors wrapping a screen-reader "Share"
// span, and on the xerox skin with four of them ("Share 2L Operations Manager
// with Facebook", "... with LinkedIn", "... with Twitter", "... with a friend
// via e-mail"). Read positionally those became part of the location, which is
// the kind of defect that never fails a build and quietly corrupts a filter.
//
// A span inside a link is a link's label rather than one of the row's fields, on
// every template measured, so the rule is structural rather than a list of
// strings to ignore.
func avatureSpans(row *html.Node) []*html.Node {
	var (
		spans []*html.Node
		walk  func(*html.Node)
	)

	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}

			if c.Data == "a" {
				continue
			}

			if c.Data == "span" {
				spans = append(spans, c)
			}

			walk(c)
		}
	}

	if row.Type == html.ElementNode && row.Data == "a" {
		return nil
	}

	walk(row)

	return spans
}

// avatureField holds the pieces of one result row that this project stores.
type avatureField struct {
	Location    string
	Posted      string
	Requisition string
}

// avatureRowFields reads a result row under the three shapes that are safe to
// read, and refuses to guess at anything else.
//
// The ordering is a trust ordering. A row that names its fields with classes is
// read from the classes and nothing else. A row that does not is read
// positionally, but only inside its subtitle container and only when at least
// one span in that container names itself -- "Ref #22821", "Posted
// Jul-28-2026", "City:". Without that evidence nothing is published.
//
// The gate is not caution for its own sake; it was measured. Read positionally
// with no gate, across all 87 sites, jobs.colorado.edu produced the location
// "Student Life Communication & Marketing, Requisition Number: 73770, Boulder,
// Colorado, University Staff, Full-Time, Posting End Date: 04-Aug-2026, Date
// Posted: 28-Jul-2026" and traderjoes.avature.net produced "Share Crew with
// Facebook Share Crew with Twitter Share Crew with Linkedin". Both are worse
// than no location at all: "unknown" is a fact a filter can act on, and a
// location field holding a department, a deadline and four share buttons is one
// that silently matches the wrong searches.
func avatureRowFields(row *html.Node) avatureField {
	var (
		fields    avatureField
		locations []string
	)

	assign := func(kind, value string) {
		if value == "" {
			return
		}

		switch kind {
		case "location":
			locations = append(locations, value)
		case "posted":
			if fields.Posted == "" {
				fields.Posted = value
			}
		case "requisition":
			if fields.Requisition == "" {
				fields.Requisition = value
			}
		}
	}

	spans := avatureSpans(row)

	if avatureHasClassedField(row) {
		for _, span := range spans {
			for class, kind := range avatureFieldClasses {
				if !icimsHasClass(span, class) {
					continue
				}

				// Several sites repeat the field name inside the classed value:
				// careers.mantech.com writes "Location: USA-SC-North
				// Charleston" in its list-item-location and
				// careers.fonterra.com writes "ref:Ref #APROPSHVN" in its
				// list-item-ref. The class already said what the field is.
				value := avatureTrimSeparators(icimsText(span))

				if rest, found := avatureCutInlineLabel(value, kind); found {
					value = rest
				}

				assign(kind, value)

				break
			}
		}

		fields.Location = strings.Join(locations, ", ")

		return fields
	}

	var (
		labelled bool
		pending  string
		unnamed  []string
	)

	for _, span := range spans {
		if !avatureInSubtitle(row, span) {
			continue
		}

		text := avatureTrimSeparators(icimsText(span))
		if text == "" {
			continue
		}

		// A label span sets the meaning of whatever comes next. The value is
		// either the rest of the enclosing element -- the xerox skin writes
		// "<span class="text--bold">City:</span> Cebu City" -- or the next span,
		// which is how astellas writes "Job ID:" "5709" "Employment Class:"
		// "Permanent" "Location:" "United Kingdom". Read without this, the
		// labels themselves became the values: astellas published the
		// requisition ":" and the location "5709, Employment Class:, Permanent,
		// Location:, United Kingdom".
		if strings.HasSuffix(text, ":") && len(text) <= avatureMaxLabelLength {
			labelled = true
			pending = avatureLabelKind(text)

			// Only when the label is alone in its element. astellas puts all
			// six of a row's spans in one <div>, where "the rest of the parent"
			// is the whole subtitle.
			if rest := avatureLabelValue(span); rest != "" {
				assign(pending, rest)

				pending = ""
			}

			continue
		}

		if pending != "" {
			assign(pending, text)

			pending = ""

			continue
		}

		lowered := strings.ToLower(text)

		if kind, ok := avatureLabelPrefix(lowered, avatureLabelledPrefixes); ok {
			labelled = true

			assign(kind, text)

			continue
		}

		if _, ok := avatureLabelPrefix(lowered, avatureIgnoredSpanPrefixes); ok {
			labelled = true

			continue
		}

		// harmanglobal and careers.mantech.com write the label inside the value
		// span itself ("Location: USA-SC-North Charleston"), which no split can
		// reach.
		if rest, found := avatureCutInlineLabel(text, "location"); found {
			labelled = true

			assign("location", rest)

			continue
		}

		// A hashtag is a syndication marker rather than a field.
		// gpshospitality ends every row with "#Snag" and "#CareerBuilder".
		if strings.HasPrefix(text, "#") {
			continue
		}

		unnamed = append(unnamed, text)
	}

	if labelled {
		locations = append(locations, unnamed...)
	}

	fields.Location = strings.Join(locations, ", ")

	return fields
}

// avatureCutInlineLabel strips a field name written inside the value itself,
// when it names the field the value was already known to be.
func avatureCutInlineLabel(text, kind string) (string, bool) {
	label, rest, ok := strings.Cut(text, ":")
	if !ok || len(label)+1 > avatureMaxLabelLength || avatureLabelKind(label+":") != kind {
		return "", false
	}

	if rest = avatureTrimSeparators(rest); rest == "" {
		return "", false
	}

	return rest, true
}

// avatureLabelValue returns the text a label span's own element holds after the
// label, or "" when the element holds more spans and therefore cannot be a
// single label/value pair.
func avatureLabelValue(span *html.Node) string {
	parent := span.Parent
	if parent == nil {
		return ""
	}

	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "span" && c != span {
			return ""
		}
	}

	return avatureTrimSeparators(strings.TrimPrefix(icimsText(parent), icimsText(span)))
}

// avatureMaxLabelLength bounds what counts as a field label rather than a value
// that happens to end in a colon.
const avatureMaxLabelLength = 24

// avatureFieldDiscard is the field name for a label this project has nowhere to
// put. It has to be a name rather than the empty string: the empty string means
// "nothing is pending", and conflating the two let astellas's "Employment
// Class:" release its value "Permanent" into the location.
const avatureFieldDiscard = "discard"

// avatureLabelKind maps a label span's text onto the field its value belongs
// in, returning "" for the labels this project has nowhere to put -- astellas
// publishes "Employment Class:", whose value must be consumed and discarded
// rather than falling through into the location.
func avatureLabelKind(label string) string {
	lowered := strings.ToLower(strings.TrimSuffix(label, ":"))

	switch {
	case strings.HasSuffix(lowered, "location"), lowered == "city", lowered == "country",
		strings.HasPrefix(lowered, "state"), strings.HasPrefix(lowered, "province"):
		return "location"
	case strings.Contains(lowered, "job id"), strings.Contains(lowered, "ref"),
		strings.Contains(lowered, "req"):
		return "requisition"
	case strings.Contains(lowered, "posted"):
		return "posted"
	default:
		return avatureFieldDiscard
	}
}

// avatureTrimSeparators strips the punctuation Avature templates put between
// fields, which arrives attached to the value on several skins: mclaren writes
// "Woking \u00b7", berenberg writes "\u2022 Frankfurt" and gpshospitality writes
// "Jackson.".
func avatureTrimSeparators(text string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(text), " .,|\u00b7\u2022-"))
}

// avatureLabelPrefix reports which field a span's text names itself as.
func avatureLabelPrefix(lowered string, labels map[string]string) (string, bool) {
	for prefix, kind := range labels {
		if strings.HasPrefix(lowered, prefix) {
			return kind, true
		}
	}

	return "", false
}

// avatureInSubtitle reports whether a span sits inside the row's subtitle
// container, which is where every template that publishes unclassed metadata
// puts it: article__header__text__subtitle on the article and table skins,
// list__item__text__subtitle on the card skin.
//
// Everything else in a row is chrome. Bounding the positional read to this
// container is what keeps a department, a closing date and a row of share
// buttons out of [internal.JobPosting.Location].
func avatureInSubtitle(row, span *html.Node) bool {
	for n := span; n != nil && n != row.Parent; n = n.Parent {
		if n.Type != html.ElementNode {
			continue
		}

		for _, class := range strings.Fields(icimsAttr(n, "class")) {
			if strings.Contains(strings.ToLower(class), "subtitle") {
				return true
			}
		}
	}

	return false
}

// avatureHasClassedField reports whether a row published any of the semantic
// span classes in [avatureFieldClasses].
func avatureHasClassedField(row *html.Node) bool {
	for _, span := range avatureSpans(row) {
		for class := range avatureFieldClasses {
			if icimsHasClass(span, class) {
				return true
			}
		}
	}

	return false
}

// avatureDateLayouts are the two orderings measured on 2026-07-29, both of them
// unambiguous because the month is spelled rather than numbered.
//
// The distinction that would need a per-board inference -- 3/7/2026 -- does not
// arise here, which is the one respect in which this platform is kinder than
// iCIMS and BrassRing.
var avatureDateLayouts = []string{"2-Jan-2006", "Jan-2-2006", "02-Jan-2006", "Jan-02-2006"}

// avatureTime parses a "Posted 23-Jul-2025" or "Posted Jul-28-2026" span.
//
// The result is UTC because the board publishes no zone, which is what
// [internal.JobPosting.PostedAt] documents every adapter to do. A value that is
// not a date, including the relative prose an older skin can carry, yields the
// zero time rather than a guess.
func avatureTime(value string) time.Time {
	value = strings.Join(strings.Fields(value), " ")

	for _, prefix := range []string{"Posted:", "Posted"} {
		if trimmed, found := strings.CutPrefix(value, prefix); found {
			value = strings.TrimSpace(trimmed)

			break
		}
	}

	if value == "" {
		return time.Time{}
	}

	for _, layout := range avatureDateLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// avatureRequisitionID strips the label an Avature row writes in front of the
// employer's own number, leaving "22821" rather than "Ref #22821".
//
// It is [internal.JobPosting.RequisitionID] rather than ExternalID: the URL id
// is Avature's, and the two differ on every site that publishes both (ally's
// posting 17055 carries Ref #22821).
func avatureRequisitionID(value string) string {
	value = strings.TrimSpace(value)

	for _, prefix := range []string{"Ref #", "Ref#", "Ref ", "Job ID", "Job Id", "JobID", "Req #", "Req#"} {
		if trimmed, found := strings.CutPrefix(value, prefix); found {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed), "#"))
		}
	}

	return value
}

// avatureNextOffset returns the record offset the career site says comes next,
// which is the smallest jobOffset it links to that is greater than the current
// one.
//
// Reading every pagination link rather than only the one marked "Next" is not
// belt-and-braces, it is required: talent.stjude.org publishes numbered page
// links for offsets 20, 40, 60, 80 and 100 and marks none of them as next, and
// clinicianjobs.advocatehealth.org puts the paginationNextLink class on the
// <li> rather than on the <a> inside it. Matching either would have stopped
// both boards after one page.
//
// Requiring a strict increase is what keeps the walk terminating. A past-the-end
// offset on this platform answers HTTP 200 with a "No jobs found" page that
// still carries a Next link pointing at offset 6, measured on ally.avature.net
// at offsets 66, 72 and 600. A walk that followed it would oscillate for as long
// as the crawl deadline allowed.
func avatureNextOffset(doc *html.Node, offset int) (int, bool) {
	next := 0

	for _, anchor := range avatureAnchors(doc) {
		parsed, err := url.Parse(icimsAttr(anchor, "href"))
		if err != nil {
			continue
		}

		raw := parsed.Query().Get("jobOffset")
		if raw == "" {
			continue
		}

		candidate, err := strconv.Atoi(raw)
		if err != nil || candidate <= offset {
			continue
		}

		if next == 0 || candidate < next {
			next = candidate
		}
	}

	return next, next > 0
}

// avatureFetch performs one search-page request.
//
// It exists instead of [fetchHTML] for one reason: HTTP 202 is not a failure on
// this platform. docs/research/ats-platform-survey.md records that Avature
// answers a bot-walled tenant with 202 and an empty body, and the instruction
// this adapter was written to is that such a tenant must be dropped rather than
// counted as a failed source. fetchHTML turns every non-200 into an error, which
// would put a bot wall and a broken board in the same bucket.
//
// Nothing else is relaxed. A 404, a 500 or a transport failure is still an
// error, because those are how a career site that has moved or been retired
// shows up and this project would rather see 87 sources fail loudly than
// silently report zero.
func avatureFetch(ctx context.Context, httpClient *http.Client, company, pageURL string) (*html.Node, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request for %s company %q: %w", avaturePlatform, company, err)
	}

	req.Header.Set("Accept", "text/html")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to make request to %s for company %q: %w", avaturePlatform, company, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, true, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status code from %s for company %q: %s", avaturePlatform, company, resp.Status)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse HTML from %s for company %q: %w", avaturePlatform, company, err)
	}

	return doc, false, nil
}

// avaturePagePostings turns one search page into postings, and reports the host
// any posting link on it pointed at instead of this site's.
func avaturePagePostings(key, slug, host string, doc *html.Node) ([]*internal.JobPosting, string) {
	var (
		postings  []*internal.JobPosting
		elsewhere string
		seen      = make(map[string]bool)
	)

	for _, anchor := range avatureAnchors(doc) {
		canonical, otherHost, ok := avatureCanonicalURL(host, icimsAttr(anchor, "href"))
		if !ok {
			if otherHost != "" && elsewhere == "" {
				elsewhere = otherHost
			}

			continue
		}

		title := icimsText(anchor)
		if title == "" {
			// Some templates wrap the card in a second, textless link to the
			// same posting. The titled one is the row's heading and comes
			// first in document order on every site measured.
			continue
		}

		if seen[canonical] {
			continue
		}

		seen[canonical] = true

		posting := &internal.JobPosting{
			Company:  slug,
			URL:      canonical,
			Title:    title,
			Location: "unknown",

			ExternalID: avatureExternalID(canonical),
			Source: internal.PostingSource{
				Platform: avaturePlatform,
				Key:      key,
			},
		}

		fields := avatureRowFields(avatureRow(host, anchor))

		if fields.Location != "" {
			posting.Location = fields.Location
		}

		posting.PostedAt = avatureTime(fields.Posted)
		posting.RequisitionID = avatureRequisitionID(fields.Requisition)

		postings = append(postings, posting)
	}

	return postings, elsewhere
}

// Avature returns all of the job postings for one Avature career site, or an
// error if there was a problem making the request or parsing the response.
//
// key is a "slug,host,section" triple, see [AvatureCareerSites].
//
// # Pagination
//
// jobOffset is a record offset. The walk follows the site's own pagination links
// and requires the offset to strictly increase; when a page publishes no
// pagination at all it advances by the number of postings that page yielded,
// which can overlap but cannot skip. Four things bound it, because this project
// has been bitten by an offset loop that trusted a board:
//
//   - a page yielding no postings ends the walk, which is what a past-the-end
//     offset produces here;
//   - the next offset must be strictly greater than the current one, which is
//     what stops the "No jobs found" page's Next link from sending the walk
//     back to offset 6;
//   - [pageRepeatGuard] ends the walk when a page repeats an earlier page's
//     posting ids;
//   - [avatureMaxPages] holds unconditionally.
//
// # Career sites larger than the paging window
//
// Avature stops paging at [avatureResultWindow] records and answers deeper
// offsets with an HTTP 200 error page, which is byte-for-byte as plausible as
// the end of a board. A walk cut off there reports an error rather than the
// partial total, because a source that quietly publishes 2,010 of an employer's
// 5,000 openings is worse than a source that fails: the number looks fine.
//
// # Bot walls and login walls
//
// A tenant behind Avature's bot challenge answers HTTP 202 with an empty body,
// and one behind a login wall answers 200 with a page carrying no posting links.
// Both yield nothing and neither is an error: a career site this project cannot
// read is not the same event as a career site that broke, and reporting them
// alike would put a permanent 87-source error floor into every crawl summary.
//
// A page whose only posting links point at a DIFFERENT host is an error, and is
// the one shape that gets one. It means the site now redirects somewhere else,
// which 59 of these 87 already did once; yielding those links anyway is how a
// crawler double counts an employer under two keys, and staying silent is how a
// moved board looks like an employer who stopped hiring.
func Avature(ctx context.Context, httpClient *http.Client, key string) internal.Jobs {
	// https://$host$section/SearchJobs/?jobOffset=0
	return func(yield func(*internal.JobPosting, error) bool) {
		slug, host, section, ok := avatureSite(key)
		if !ok {
			yield(nil, fmt.Errorf("invalid %s key %q: want \"slug,host,section\"", avaturePlatform, key))

			return
		}

		var (
			guard     pageRepeatGuard
			seen      = make(map[string]bool)
			offset    = 0
			elsewhere string
		)

		for requests := 0; requests < avatureMaxPages; requests++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			doc, walled, err := avatureFetch(ctx, httpClient, slug, avatureSearchURL(host, section, offset))
			if err != nil {
				yield(nil, err)

				return
			}

			if walled {
				return
			}

			postings, other := avaturePagePostings(key, slug, host, doc)
			if other != "" && elsewhere == "" {
				elsewhere = other
			}

			if len(postings) == 0 {
				// A career site larger than Avature's paging window stops
				// dead here, at HTTP 200, with an error page that is
				// indistinguishable from the end of a board unless the offset
				// is checked. Reporting the postings gathered so far as this
				// employer's total is the one outcome this project will not
				// accept, so the source fails instead.
				//
				// A site holding exactly one window's worth of postings trips
				// this too, since the request past its last full page is also
				// past the window. That false positive costs one visible
				// failure on a source that would otherwise be complete, which
				// is the right way round.
				if offset > avatureResultWindow {
					yield(nil, fmt.Errorf("incomplete response from %s for company %q: %d postings read and the site is still returning full pages, but Avature stops paging at %d records, so this career site cannot be crawled to the end",
						avaturePlatform, slug, len(seen), avatureResultWindow))
				}

				break
			}

			ids := make([]string, 0, len(postings))

			for _, posting := range postings {
				ids = append(ids, posting.URL)

				if seen[posting.URL] {
					continue
				}

				seen[posting.URL] = true

				if !yield(posting, nil) {
					return
				}
			}

			// Checked after the postings are yielded rather than before, so a
			// site that repeats a page still contributes that page's postings
			// once. The repeat is what ends the walk, not what discards it.
			if guard.repeated(ids) {
				break
			}

			next, ok := avatureNextOffset(doc, offset)
			if !ok {
				next = offset + len(postings)
			}

			offset = next
		}

		if len(seen) == 0 && elsewhere != "" {
			yield(nil, fmt.Errorf("unexpected response shape from %s for company %q at %s: every posting link points at %s, so this career site has moved",
				avaturePlatform, slug, avatureSearchURL(host, section, 0), elsewhere))
		}
	}
}
