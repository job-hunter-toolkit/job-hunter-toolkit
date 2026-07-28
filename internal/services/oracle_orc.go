package services

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

const (
	// oracleCloudPageSize is how many requisitions are asked for per request.
	//
	// 200 is not a preference, it is the server's cap, measured: a request for
	// limit=500 and a request for limit=1000 against
	// ejwl.fa.us2.oraclecloud.com/CX_2 each came back with exactly 200
	// requisitions. Asking for more only inflates the URL.
	//
	// The list response carries title, primary location and posted date, so
	// unlike every reference implementation of this API that this project's
	// research looked at, no per-posting detail request is needed: those exist
	// only to fetch description prose, which [internal.JobPosting] does not
	// store. Measured across the whole registered set that is 118 postings per
	// HTTP request, the second-cheapest lane in the project after
	// SuccessFactors, and the reason it is worth adding to a crawl that already
	// misses its deadline.
	oracleCloudPageSize = 200

	// oracleCloudMaxWindow is the deepest row this API will serve, measured.
	//
	// Oracle enforces "offset + limit <= 10000" and answers anything past it
	// with an empty requisition list AND a TotalJobsCount of 0 — an HTTP 200
	// that looks exactly like a board with nothing open. Bisected against
	// eluq.fa.us2.oraclecloud.com/CX_2001 (Kroger, ~15,100 open reqs):
	// offset=9800&limit=200 serves 200 rows, offset=9900&limit=200 serves none,
	// offset=9900&limit=100 serves 100, offset=9999&limit=1 serves 1 and
	// offset=10000&limit=1 serves none. Reproduced on ejwl (Marriott) and egud
	// (AutoZone). It is the shape of an Elasticsearch max_result_window, and it
	// is a property of the platform, not of a tenant.
	//
	// The consequence is that the nine tenants in the candidate file with more
	// than 10,000 open requisitions are reachable only down to their 10,000th,
	// sorted newest-first. That truncation is deliberate and silent-by-design:
	// yielding 10,000 real postings is the correct outcome, and reporting it as
	// an error would mark Kroger a permanently failing source and push the
	// Source Health workflow toward its 35%-failure alarm for a platform that is
	// working exactly as documented. What must never happen is walking past the
	// wall and mistaking the zeroed count for an empty board, which is why
	// offsets stop here rather than on the tenant's own total.
	oracleCloudMaxWindow = 10000

	// oracleCloudPageFetchers bounds how many page requests are in flight for
	// one tenant at a time.
	//
	// Oracle tenants are host-isolated: every tenant has its own Fusion
	// Applications host and httpx.servicePolicyFor deliberately does not group
	// *.oraclecloud.com into a shared bucket, so each tenant gets its own
	// limiter key and this bound is per employer rather than global. The
	// measured shape backs that up — the 1,203 registered tenants sit on 752
	// distinct hosts.
	//
	// It is deliberately equal to httpx's per-service limit for this suffix. The
	// limiter, not this constant, is the politeness ceiling: a larger value here
	// would not send more requests, it would only park more goroutines on the
	// limiter's semaphore. This is the same reasoning, and the same number, as
	// [workdayPageFetchers].
	oracleCloudPageFetchers = 4

	// oracleCloudMaxPages bounds how many pages a single tenant may be asked
	// for, as a backstop rather than as the working limit.
	//
	// [oracleCloudMaxWindow] is what actually ends a large tenant, and at the
	// measured page size of 200 it allows 50 pages. This only binds if a tenant
	// serves far fewer rows per page than it was asked for — a real behaviour in
	// this ecosystem, which is why the offset advances by what a page held
	// rather than by what was requested — and it stops a tenant serving, say,
	// one row per page from spending 10,000 requests inside its window.
	//
	// Unlike the window, being cut short here means the response did not look
	// the way this adapter expects, so it is reported rather than passed off as
	// the end of a board.
	oracleCloudMaxPages = 500
)

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
// # How this list was chosen
//
// Every one of the 1,552 triples staged at
// testdata/candidates/oracle_orc_tenants.txt was probed live on 2026-07-28, one
// request each against the list endpoint, and the results are what this list is
// built from. The counts below are that measurement, not the candidate file's
// upstream annotations, which were never checked by this project.
//
//	probed   1,552
//	ok       1,517   items[0].TotalJobsCount above zero
//	empty       35   the promotion rule's own bar, so not registered
//	dead         0   no 404, no 410, no NXDOMAIN
//	error        0
//
// 314 of the 1,517 live triples are held back, and the reasons are worth stating
// because each is a way this platform can inflate a posting count without
// publishing a job:
//
//   - 249 are a second, third or fourth site number on a fa-host whose corpus
//     this project already reaches through another entry. Their sampled
//     requisition ids are identical to the kept entry's, so they are the same
//     openings behind a different site — a language variant, a legacy site, a
//     brand alias. [internal.Dedupe] keys on URL and the site number is IN the
//     candidate-experience URL, so nothing downstream would catch the double
//     count. Together they are ~131,000 postings that do not exist twice.
//
//   - 51 are a second site for a slug already registered, held back because a
//     source key must map to one company name.
//
//   - 8 are employers this project already crawls on another platform, where the
//     same job has a different URL on each route and would be counted twice:
//     Marriott, Mount Sinai and Conduent on Jibe, Dell and Nationwide on
//     Workday, Amex on SuccessFactors, and two small Ashby boards. This is a
//     routing decision, not a rejection; whoever can compare the two routes'
//     live counts should pick one and delete the other. The research behind this
//     adapter expects the Oracle route to be the more complete of the two,
//     because it is the employer's own ATS rather than a career-site front end.
//
//   - 6 are eubt.fa.us6.oraclecloud.com, which the candidate file lists under
//     six slugs (oracle, cetemplate, cx500, supersiteuser2, eubt,
//     candidateexperience031219) each reporting the same 78,431 requisitions —
//     five times the largest real employer in the file. It is Oracle's own
//     candidate-experience benchmark tenant: every requisition is titled
//     ZBEN_FRCE_Java_Developer or ZBEN_SVF_FRCE_Configuration_Manager with a
//     timestamp suffix, located in "CA, United States" or "Germany", dating back
//     years. Registering it would have put ~60,000 machine-generated postings
//     into the corpus and made it the biggest employer this project covers.
//
// # What this costs and what it buys
//
// 1,203 tenants on 752 distinct hosts, 267,066 postings reachable, 2,263 HTTP
// requests per crawl — 118 postings per request, measured, which is the second
// cheapest lane in the project after SuccessFactors. "Reachable" is doing real
// work in that sentence: see [oracleCloudMaxWindow], which puts nine of these
// employers out of reach past their 10,000th open requisition and accounts for
// the gap between the 847,327 requisitions these sites report and the 267,066
// this adapter can actually walk to.
//
// The distribution is long-tailed and worth knowing before budgeting: the median
// tenant has 30 open requisitions and 881 of the 1,517 live ones have fewer than
// 50, so most entries here are a single request. The top 25 are 23% of the
// requests and the top 100 are 42%.
//
// Entries are ordered by measured size, largest first, which is also roughly the
// order of requests each costs. The counts themselves are deliberately not
// written down per line: a site's TotalJobsCount moved between two requests
// minutes apart during this measurement, so a comment claiming one would be
// wrong by the next day and there is no way to tell a stale annotation from a
// shrinking employer.
var OracleCloudTenants = []string{
	"kroger,eluq.fa.us2.oraclecloud.com,CX_2001",
	"kotakadditional,hcbt.fa.em2.oraclecloud.com,CX_1001",
	"autozone,egud.fa.us2.oraclecloud.com,CX_1",
	"jpmorgan,jpmc.fa.oraclecloud.com,CX_1001",
	"albertsons,eofd.fa.us6.oraclecloud.com,CX_1001",
	"tatacapital,eofh.fa.em2.oraclecloud.com,CX_3001",
	"sallybeauty,eigx.fa.us6.oraclecloud.com,CX_2",
	"hilton,efet.fa.us2.oraclecloud.com,CX_1",
	"wsp,emit.fa.ca3.oraclecloud.com,CX_2001",
	"lifepoint,ibnjjb.fa.ocs.oraclecloud.com,CX_1",
	"aeo,hcml.fa.us2.oraclecloud.com,AEO-Careers",
	"macys,ebwh.fa.us2.oraclecloud.com,CX_1001",
	"exltalentacquisitionteam,fa-ewjt-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"asterdmhealthcare,hcdt.fa.us2.oraclecloud.com,CX",
	"tenet,eodr.fa.us2.oraclecloud.com,CX_1",
	"jobsatacosta,eczy.fa.us2.oraclecloud.com,CX_1",
	"ihg,fa-evax-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"sitiodeoportunidadesempleogrupocoppel,fa-eqwz-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"chs,fa-evxo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"photon,fa-ertb-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"chilis,fa-etjg-saasfaprod1.fa.ocs.oraclecloud.com,Chilis",
	"tenethealthcarecorporation,eodr-dev5.fa.us2.oraclecloud.com,CX_1001",
	"oracle,eeho.fa.us2.oraclecloud.com,jobsearch",
	"encompasshealth,ibwsjb.fa.ocs.oraclecloud.com,CX_1",
	"vertiv,egup.fa.us2.oraclecloud.com,CX",
	"abm,eiqg.fa.us2.oraclecloud.com,CX_1001",
	"sherwinwilliams,ejhp.fa.us6.oraclecloud.com,CX_2",
	"healthcareersinsaskca,emqk.fa.ca3.oraclecloud.com,CX_1001",
	"quest,hdox.fa.us6.oraclecloud.com,CX_1",
	"learningcare,ejql.fa.us6.oraclecloud.com,CX",
	"brookdale,ibmwjb.fa.ocs.oraclecloud.com,CX_1",
	"providence,evac.fa.us2.oraclecloud.com,CX_1",
	"ehc,ibwsjb-dev2.fa.ocs.oraclecloud.com,CX_1",
	"startek,fa-evuf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"bny,eofe.fa.us2.oraclecloud.com,CX_1",
	"baptist,fa-ewpe-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"penske,fa-euyk-saasfaprod1.fa.ocs.oraclecloud.com,PenskeCareers",
	"waste-management,emcm.fa.us2.oraclecloud.com,CX_4001",
	"csb,iaamcp.fa.ocs.oraclecloud.com,CX_1001",
	"adventisthealth,ecvz.fa.us2.oraclecloud.com,CX_1",
	"stantec,hdhl.fa.us6.oraclecloud.com,CX_1",
	"caesars,edmn.fa.us2.oraclecloud.com,CX_1",
	"clubcorp,ecwl.fa.us2.oraclecloud.com,CX",
	"mayoclinic,fa-euwp-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"arcadis,ebcs.fa.em2.oraclecloud.com,CX_1",
	"honeywell,ibqbjb.fa.ocs.oraclecloud.com,CX_1",
	"northwell,eppr.fa.us2.oraclecloud.com,CX_2",
	"securitassecurityservices,ekaw.fa.us2.oraclecloud.com,CX",
	"fortis,fa-ermg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"cleanharbors,epyc.fa.us2.oraclecloud.com,CLH-US",
	"candidateexperiencelateral,hdpc.fa.us2.oraclecloud.com,CX_3002",
	"monro,ibqmjb.fa.ocs.oraclecloud.com,CX_1",
	"sanaklinikenag,fa-eycl-saasfaeuraprod1.fa.ocs.oraclecloud.com,CX_4025",
	"hiltongrandvacations,efuq.fa.us6.oraclecloud.com,HiltonGrandVacations",
	"vitas,ejrz.fa.us2.oraclecloud.com,CX_5001",
	"wood,ehif.fa.em2.oraclecloud.com,CX_1",
	"kpmgkgs,ejgk.fa.em2.oraclecloud.com,CX_3",
	"ekcm,ekcm.fa.us6.oraclecloud.com,CX",
	"raleys,fa-epss-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"williamssonoma,ehac.fa.us6.oraclecloud.com,CX_1",
	"wellspan,fa-evzu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"staples,fa-exhh-saasfaprod1.fa.ocs.oraclecloud.com,StaplesInc",
	"cummins,fa-espx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"adani,eibd.fa.em2.oraclecloud.com,CX_1",
	"aerospace,icfcjb.fa.ocs.oraclecloud.com,CX_4001",
	"edel,edel.fa.us2.oraclecloud.com,CX_2001",
	"atlantichealth,erqh.fa.us2.oraclecloud.com,CX_1001",
	"warbyparker,fa-evdi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"cedarssinai,hdkk.fa.us6.oraclecloud.com,CX_2001",
	"ford,efds.fa.em5.oraclecloud.com,CX_1",
	"omegaexternalportal,fa-equm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"omegaph,fa-equm-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"uwhealth,eimy.fa.us6.oraclecloud.com,CX_1",
	"arcelormittal,emfg.fa.em4.oraclecloud.com,CX_4001",
	"mortensonexternal,fa-esgu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"fmolhealth,eqtm.fa.us2.oraclecloud.com,CX_3001",
	"emerson,hdjq.fa.us2.oraclecloud.com,CX_1",
	"metronashvillepublicschools,ibqhjb.fa.ocs.oraclecloud.com,CX_1001",
	"adt,fa-erqb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"bloomingdales,ebwh.fa.us2.oraclecloud.com,CX_1002",
	"howmet,fa-exty-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"adventregions,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,regions-hospital",
	"global,fa-evjm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1003",
	"bgis,fa-evcg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"hertz,fa-evlf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"athome,hdiy.fa.us2.oraclecloud.com,CX",
	"join,fa-eoic-saasfaprod1.fa.ocs.oraclecloud.com,CX_12001",
	"amplifon,efuf.fa.em2.oraclecloud.com,CX_1",
	"wesco,eklm.fa.us2.oraclecloud.com,CX",
	"mscontingentworker,fa-eqid-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"iiviaerospacedefensecoherent,hcwp.fa.us2.oraclecloud.com,CX_2004",
	"inova,elar.fa.us2.oraclecloud.com,CX_1",
	"nov,egay.fa.us6.oraclecloud.com,CX_1",
	"onsemi,hctz.fa.us2.oraclecloud.com,CX_1001",
	"rh,hcqq.fa.us2.oraclecloud.com,CX_5004",
	"chubbexternal,fa-ewgu-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"wtwexternal,eedu.fa.em3.oraclecloud.com,CX_1003",
	"mcleodsection,erym.fa.us6.oraclecloud.com,CX_1",
	"eclerx,fa-ewji-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"op,fa-esfc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"downer,fa-exfs-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"integrishealth,ertr.fa.us2.oraclecloud.com,CX_3001",
	"allcommunityjobs,eexs.fa.us2.oraclecloud.com,CX_11009",
	"nokia,fa-evmr-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"nextplc,ekeq.fa.em2.oraclecloud.com,CX_3001",
	"mcdermott,edsv.fa.us2.oraclecloud.com,CX_1",
	"kpmgindia,ejgk.fa.em2.oraclecloud.com,CX_1",
	"citizens,hcgn.fa.us2.oraclecloud.com,CX_1",
	"trihealth,fa-evly-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"landmark,efhi.fa.em3.oraclecloud.com,CX_1",
	"jumpinbolsadetrabajointercorpretail,egjl.fa.us6.oraclecloud.com,CX_1001",
	"ti,edbz.fa.us2.oraclecloud.com,CX",
	"ejov,ejov.fa.ca2.oraclecloud.com,CX",
	"utennessee,fa-ewlq-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"dps,esgj.fa.us2.oraclecloud.com,CX_1001",
	"cimbmalaysia,ejox.fa.ap1.oraclecloud.com,CX_1",
	"nemours,epyz.fa.us2.oraclecloud.com,CX_1",
	"dpworld,ehpv.fa.em2.oraclecloud.com,CX_1",
	"grupomateus,fa-exvn-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"rlcarriers,erhk.fa.us2.oraclecloud.com,CX_1",
	"summitfiresecurity,fa-exxm-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"cottonon,ekxm.fa.ap1.oraclecloud.com,CX",
	"molina,hckd.fa.us2.oraclecloud.com,CX_1",
	"ucm,fa-etnf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"estes,ecwr.fa.us2.oraclecloud.com,work4estes",
	"stobuilding,fa-exrr-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"nha,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"pearsoncandidateexperience,hccz.fa.em3.oraclecloud.com,CX_2",
	"svkmenterprize,fa-elxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1008",
	"ipglobal,iazbqy.fa.ocs.oraclecloud.com,CX_1",
	"mashreq,hcld.fa.em2.oraclecloud.com,CX_1",
	"artech,fa-erqf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"exp,elcn.fa.us2.oraclecloud.com,CX",
	"edyo,edyo.fa.us2.oraclecloud.com,CX",
	"ulsolutionsexternal,fa-eups-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"texaschildrens,eohh.fa.us2.oraclecloud.com,CX",
	"careonenew,fa-eqgc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"stjosephshealth,fa-eyip-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"rbglobal,fa-exew-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"bdousaexperienced,ebqb.fa.us2.oraclecloud.com,CX_1",
	"christhospitalhealthnetwork,fa-etxt-saasfaprod1.fa.ocs.oraclecloud.com,CX_5",
	"citcolimited,fa-euxc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"lomalinda,egln.fa.us2.oraclecloud.com,CX",
	"flexstaff,eppr.fa.us2.oraclecloud.com,CX_1",
	"chartwellenglish,hcrw.fa.us2.oraclecloud.com,CX_1",
	"dtcccandidateexperience,ebxr.fa.us2.oraclecloud.com,CX_1",
	"intertekglobal,hcog.fa.em2.oraclecloud.com,CX_2001",
	"uchealth,eswt.fa.us6.oraclecloud.com,CX_1001",
	"barrickminingcorporation,ehkn.fa.ca2.oraclecloud.com,CX_1001",
	"apparel,ediu.fa.em2.oraclecloud.com,CX_1013",
	"ptbankcimbniaga,ejox.fa.ap1.oraclecloud.com,CX_5",
	"zachry,fa-evfm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1029",
	"sinclairbroadcast,edyy.fa.us2.oraclecloud.com,CX_2002",
	"hackett,eeih.fa.us2.oraclecloud.com,CX_6",
	"faeoccsaasfaprod1,fa-eocc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1007",
	"atcbiz,ebez.fa.us2.oraclecloud.com,CX_1",
	"tiffanysrtesting,eljs.fa.us2.oraclecloud.com,CX_11001",
	"mtc,fa-euud-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"elij,elij.fa.em2.oraclecloud.com,CX",
	"hearst,eevd.fa.us6.oraclecloud.com,CX_1",
	"kroll,hcxs.fa.us2.oraclecloud.com,CX_1",
	"mosaichealthsystem,ibcjqy.fa.ocs.oraclecloud.com,CX_2",
	"retal,fa-etzd-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"shaner,ebwv.fa.us2.oraclecloud.com,CX_1",
	"centracare,elyb.fa.us2.oraclecloud.com,CX_1001",
	"bip,fa-etjb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"wingstop,iaxmqy.fa.ocs.oraclecloud.com,Wingstop-Restaurant",
	"hdhe,hdhe.fa.em3.oraclecloud.com,CX",
	"memorialgulfport,wearememorial-ibrkjb.fa.ocs.oraclecloud.com,Careers",
	"fortive,ejta.fa.us6.oraclecloud.com,CX_1",
	"opexternalsection,hcnq.fa.em2.oraclecloud.com,CX",
	"firstenergy,fa-etjd-saasfaprod1.fa.ocs.oraclecloud.com,FirstEnergyCareers",
	"zensar,fa-etvl-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"daimlertruck,fa-exdu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"hillminimal112022,hcib.fa.us2.oraclecloud.com,CX_1001",
	"newmark,hdow.fa.us6.oraclecloud.com,CX_1001",
	"soprema,fa-eshb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1003",
	"connecticutchildrens,fa-evav-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"stationcasinos,ejfh.fa.us6.oraclecloud.com,StationCasinos",
	"weatherford,fa-exmi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"tamer,eenp.fa.em3.oraclecloud.com,CX_1",
	"gallifordtry,cbct.fa.em2.oraclecloud.com,CX_1",
	"baylorscottwhite,ejof.fa.us2.oraclecloud.com,BaylorCareers",
	"churchofjesuschristoflatterdaysaints,epej.fa.us2.oraclecloud.com,CX_2001",
	"hexaware,fa-etqo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"firstsolar,fa-esbv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"olatheschools,fa-ewbu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"accoengineeredsystems,ekkt.fa.us2.oraclecloud.com,CX_1",
	"coopercompanies,hcjy.fa.us2.oraclecloud.com,CX",
	"alghurair,iaaxey.fa.ocs.oraclecloud.com,CX_1",
	"oceaneering,ebfr.fa.us2.oraclecloud.com,CX_3001",
	"usf,fa-ewkd-saasfaprod1.fa.ocs.oraclecloud.com,USF",
	"crawfordjoblistingsglobal,fa-esau-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"blackbox,eoje.fa.us2.oraclecloud.com,CX_1001",
	"syracuseschools,ibmljb.fa.ocs.oraclecloud.com,CX_1005",
	"scout,ehzq.fa.us2.oraclecloud.com,CX_4",
	"hologic,ebwb.fa.us2.oraclecloud.com,CX",
	"hormel,ekkh.fa.us2.oraclecloud.com,CX_1",
	"ricoh,cbha.fa.us2.oraclecloud.com,CX_1",
	"synlabglobal,fa-eugp-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"mauserpackagingsolutions,hdpn.fa.us6.oraclecloud.com,CX_1",
	"michaelbakerinternational,ebxs.fa.us2.oraclecloud.com,CX_2",
	"carnival,eicl.fa.em5.oraclecloud.com,CX_2",
	"usfuniversityofsouthflorida,fa-ewkd-dev1-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"standardaero,cva.fa.us1.oraclecloud.com,CX_3",
	"savencia,eoct.fa.em2.oraclecloud.com,CX_2005",
	"akamai,fa-extu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"digitalrealty,hdep.fa.us2.oraclecloud.com,CX",
	"uthealthsa,fa-eomf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1002",
	"fanatics,fa-exki-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"amnealindia,hcfa.fa.us2.oraclecloud.com,CX_5001",
	"redpath,fa-evvx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"apsexternal,fa-epop-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"tulane,tulane-ibqejb.fa.ocs.oraclecloud.com,CX_1",
	"dnv,ecyq.fa.em2.oraclecloud.com,CX_1",
	"masholdings,egmh.fa.us6.oraclecloud.com,CX_1",
	"lcc,fa-esms-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1",
	"aps,fa-ewxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"scor,fa-errt-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"linamarnew,fa-epmd-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"uwcandidateexperience,eeik.fa.us2.oraclecloud.com,CX_1",
	"chartwellfrench,hcrw.fa.us2.oraclecloud.com,CX_1001",
	"ipsos,ecqf.fa.em2.oraclecloud.com,CX_2001",
	"legrand,iadugs.fa.ocs.oraclecloud.com,CX_1001",
	"cimbthailand,ejox.fa.ap1.oraclecloud.com,CX_7",
	"pattersonuti,fa-elpm-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"taogroup,fa-euyb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"ssmc,fa-eutv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"hkbuerecruitmentsystem,fa-ewqq-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"csc,hczw.fa.us2.oraclecloud.com,CX_1001",
	"anywhere,ibmqjb.fa.ocs.oraclecloud.com,CX_1",
	"ciklum,ialmme.fa.ocs.oraclecloud.com,CX_1001",
	"americold,fa-ewwt-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"iom,fa-evlj-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"langhamhospitality,fa-eqgp-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"osumedical,osumc-iborjb.fa.ocs.oraclecloud.com,OSUMC",
	"tfg,fa-expc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"dallascounty,fa-etvc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"yum,eczd.fa.us2.oraclecloud.com,CX_1",
	"hcps,fa-exea-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"workingatsignatureaviation,hdbt-dev1.fa.us2.oraclecloud.com,CX_1",
	"carrireexternesodiaal,fa-epmr-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"bristlecone,iaagiz.fa.ocs.oraclecloud.com,CX_1",
	"fedcap,eckb.fa.us2.oraclecloud.com,CX_1001",
	"infinitiretailltd,fa-eryk-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"hearsttelevision,eevd.fa.us6.oraclecloud.com,CX_6",
	"healthpartnersghi,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_4003",
	"powell,ekcf.fa.us6.oraclecloud.com,CX_1",
	"dc,eigx.fa.us6.oraclecloud.com,CX_1",
	"napco,iaeegs.fa.ocs.oraclecloud.com,CX_1",
	"bhe,fa-essf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"technipenergies,hcxg.fa.em2.oraclecloud.com,CX_1",
	"cottagehealth,eglz.fa.us2.oraclecloud.com,CX",
	"dieboldnixdorf,eeug.fa.us6.oraclecloud.com,CX",
	"jefferies,hdid.fa.us2.oraclecloud.com,CX_1",
	"nmdc,fa-evft-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"springfieldclinic,eify.fa.us6.oraclecloud.com,CX_1004",
	"pellawindowsdoors,ebgj.fa.us2.oraclecloud.com,CX_1",
	"coherentmalaysia,hcwp.fa.us2.oraclecloud.com,CX_7009",
	"choctawnation,egoh.fa.us2.oraclecloud.com,CX_1001",
	"euroclear,don.fa.em2.oraclecloud.com,CX_1003",
	"arconic,hdnn.fa.us6.oraclecloud.com,CX",
	"mrprice,fa-etyi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"scminimal,ejia.fa.us6.oraclecloud.com,CX_1001",
	"glensfalls,hdbg.fa.us2.oraclecloud.com,CX_1",
	"gedu,geduglobal-iabmbn.fa.ocs.oraclecloud.com,CX_1",
	"bluemercury,ebwh.fa.us2.oraclecloud.com,CX_4001",
	"rogers,hdhf.fa.us6.oraclecloud.com,CX_2001",
	"candidateexperiencecampus,hdpc.fa.us2.oraclecloud.com,CX_3001",
	"regionshospitalrhsc,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"westpac,ebuu.fa.ap1.oraclecloud.com,CX",
	"tforcefreight,efjm.fa.ca2.oraclecloud.com,TForceFreight",
	"calgaryop,fa-etus-saasfaprod1.fa.ocs.oraclecloud.com,CX_2004",
	"seweurodrive,fa-erav-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"calderysopportunities,egmk.fa.us6.oraclecloud.com,CX",
	"elhr,elhr.fa.us2.oraclecloud.com,CX_1001",
	"ums,fa-ewca-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"workforum,ibqajb.fa.ocs.oraclecloud.com,CX_1",
	"ualberta,iaejup.fa.ocs.oraclecloud.com,UOA-Careers",
	"mufgpensionmarketservices,hcmn.fa.ap1.oraclecloud.com,CX_1001",
	"manitowoc,fda.fa.us1.oraclecloud.com,CX_1",
	"veriskverisk,fa-ewmy-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"adenahealth,eord.fa.us2.oraclecloud.com,CX_1001",
	"mapeicandidateexperience,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"hoffmanconstruction,efsp.fa.us6.oraclecloud.com,CX",
	"etsu,fa-evyu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"rocklandtrust,eehb.fa.us2.oraclecloud.com,CX_3001",
	"corporatetrueblue,ehnn.fa.us2.oraclecloud.com,CX_3001",
	"nsf,hcnz.fa.us2.oraclecloud.com,CX_1001",
	"hukcoburg,fa-eyjr-saasfaeuraprod1.fa.ocs.oraclecloud.com,CX_1",
	"texaswomans,ewal.fa.us8.oraclecloud.com,CX_1",
	"seha,fa-eutv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"tadweer,iaasey.fa.ocs.oraclecloud.com,CX_1",
	"eastsussexcountycouncil,fa-euru-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"americanbureauofshipping,hbbq.fa.us2.oraclecloud.com,CX_1",
	"hoag,iaucqy.fa.ocs.oraclecloud.com,CX_1",
	"coniferhealthsolutions,eodr-dev5.fa.us2.oraclecloud.com,CX_2001",
	"mariecurievolunteers,eofb.fa.em3.oraclecloud.com,CX_4001",
	"vanderbilt,ecsr.fa.us2.oraclecloud.com,CX_1",
	"tulanestudent,tulane-ibqejb.fa.ocs.oraclecloud.com,CX_2",
	"harpercollege,fa-eneh-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"overheaddoor,hcas.fa.us1.oraclecloud.com,CX_1001",
	"securitascanada,ekaw.fa.us2.oraclecloud.com,CX_1001",
	"hillside,ebrf.fa.us2.oraclecloud.com,CX_1",
	"marriottnonuscampus,ejwl.fa.us2.oraclecloud.com,CX_1001",
	"ursinuscollege,iaotqy.fa.ocs.oraclecloud.com,CX_2",
	"icumed,eduu.fa.us2.oraclecloud.com,CX_1",
	"wynnalmarjan,wam-iabkey.fa.ocs.oraclecloud.com,CX_2",
	"shelbycounty,iayzqy.fa.ocs.oraclecloud.com,CX_1",
	"gmfinancialunitedstates,fa-exvu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"wiltshire,fa-euxi-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1001",
	"mountairejobsquantcast,ebtg.fa.us2.oraclecloud.com,CX_2001",
	"childrens,egnx.fa.us2.oraclecloud.com,CX",
	"cherrycreek,enmv.fa.us2.oraclecloud.com,CX_2001",
	"lhcgvolunteer,erzb.fa.us2.oraclecloud.com,CX_1001",
	"navarro,fa-espf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"sensient,eour.fa.us2.oraclecloud.com,CX",
	"ammega,fa-eugx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"aciworldwidejobopportunities,ebwg.fa.us2.oraclecloud.com,CX",
	"hubgroup,hdat.fa.us2.oraclecloud.com,CX",
	"undp,estm.fa.em2.oraclecloud.com,CX_1",
	"apllexternal,hcut.fa.ap2.oraclecloud.com,CX_1",
	"mouser,eabw.fa.us2.oraclecloud.com,CX_1",
	"rheem,hdde.fa.us2.oraclecloud.com,CX",
	"garrettadvancingmotion,ehth.fa.em2.oraclecloud.com,CX_2001",
	"atruvia,fa-exxd-saasfaprod1.fa.ocs.oraclecloud.com,CX_3",
	"virtuos,fa-exhj-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"kent,fa-emqh-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"tgjobsitemain,iaboey.fa.ocs.oraclecloud.com,CX_2",
	"suffolkjobsdirect,eoce.fa.em3.oraclecloud.com,CX_1004",
	"cityofedinburghcouncil,iaghme.fa.ocs.oraclecloud.com,CX_1",
	"icao,estm.fa.em2.oraclecloud.com,CX_3001",
	"dtujobsside,efzu-dev8.fa.em2.oraclecloud.com,CX_2001",
	"dalmiabharat,fa-ekwr-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"cantorfitzgeraldbgc,hdow.fa.us6.oraclecloud.com,CX_1003",
	"varunbeverageslimitedvbl,rjcorphcm-iacbiz.fa.ocs.oraclecloud.com,CX_1",
	"masimo,egcu.fa.us6.oraclecloud.com,CX",
	"southerncompany,emje.fa.us6.oraclecloud.com,SouthernCompanyJobs",
	"sbicexternal,edox.fa.ap1.oraclecloud.com,CX_3001",
	"unipolexternal,hdix.fa.em3.oraclecloud.com,CX_1",
	"bankofhawaii,fa-enlf-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"frontgrade,exzj.fa.us8.oraclecloud.com,CX_1",
	"vdot,etgi.fa.us8.oraclecloud.com,CX_4005",
	"samuelson,ebct.fa.us2.oraclecloud.com,CX_3002",
	"svkmcentraloffice,fa-elxu-test-saasfaprod1.fa.ocs.oraclecloud.com,CX_1012",
	"sdu,fa-eosd-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"carmeuse,ekiz.fa.em2.oraclecloud.com,CX",
	"trabajaeninterbank,epvw.fa.us6.oraclecloud.com,CX_1",
	"forwardair,eguq.fa.us2.oraclecloud.com,MovingYouForward",
	"elcaglobal,iaaras.fa.ocs.oraclecloud.com,CX_4",
	"nexi,fa-ewwx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"fab,ehjd.fa.em2.oraclecloud.com,CX_2001",
	"aldareducation,fa-etxx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"fifecounciljobsand,ekmu.fa.em3.oraclecloud.com,CX_6009",
	"bostonbeer,hcrb.fa.us2.oraclecloud.com,CX",
	"bsc,ecge.fa.us2.oraclecloud.com,CX_1003",
	"enterprisewide,hdrc.fa.ca3.oraclecloud.com,CX_7002",
	"henrycountyschools,fa-exna-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"navyfederalcreditunion,fa-etbx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"atmus,fa-ewfi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"newlhrcexternal,evqk.fa.us8.oraclecloud.com,CX_2001",
	"lhc,erzb.fa.us2.oraclecloud.com,CX_1",
	"greatcanadian,elvk.fa.ca3.oraclecloud.com,CX_1001",
	"infoblox,efpv.fa.us6.oraclecloud.com,CX_1",
	"denso,hcwt.fa.us2.oraclecloud.com,CX",
	"layton,fa-exrr-dev2-saasfaprod1.fa.ocs.oraclecloud.com,CX_8",
	"sinch,iaings.fa.ocs.oraclecloud.com,CX_1",
	"omnicelltemp,elrj.fa.us2.oraclecloud.com,CX_9001",
	"perficient,fa-etqd-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"atco,eezy.fa.ca2.oraclecloud.com,CX",
	"incomeinsurancelimited,etvy.fa.ap2.oraclecloud.com,CX_2001",
	"orf,fa-eujo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"argano,fa-eyau-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"ebhu,ebhu.fa.us2.oraclecloud.com,CX",
	"student,ehwy.fa.us2.oraclecloud.com,CX",
	"amnealpiscataway,hcfa.fa.us2.oraclecloud.com,CX_3005",
	"novotech,fa-euzi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"civeo,fa-esyi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"summitfireprotection,fa-exxm-saasfaprod1.fa.ocs.oraclecloud.com,CX_5",
	"tallgrass,epix.fa.us2.oraclecloud.com,CX_1",
	"centrusglobal,eese.fa.us8.oraclecloud.com,CX_1",
	"cimic,elgl.fa.ap1.oraclecloud.com,CX_1001",
	"cleanharborsca,epyc.fa.us2.oraclecloud.com,CX_2001",
	"easternbank,gka.fa.us1.oraclecloud.com,CX",
	"computershare,fa-evdq-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"loraincc,ccyj.fa.us6.oraclecloud.com,CX",
	"uemedgenta,enpf.fa.ap1.oraclecloud.com,CX",
	"bentity,ejgk.fa.em2.oraclecloud.com,CX_1001",
	"anil,eibd.fa.em2.oraclecloud.com,CX_2011",
	"universityoftulsa,utulsa-ibvjjb.fa.ocs.oraclecloud.com,CX_1",
	"shannonmedical,shannonmc-iazxqy.fa.ocs.oraclecloud.com,CX_1",
	"harrahscherokee,epgr.fa.us6.oraclecloud.com,CX_1002",
	"urus,hdjm.fa.ca2.oraclecloud.com,CX_5",
	"corsair,edix.fa.us2.oraclecloud.com,CX_1",
	"worthingtonenterprises,fa-eygo-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"curia,hcug.fa.us2.oraclecloud.com,CX_2001",
	"adports,fa-ewzx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"firstwest,fa-eomj-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"hayleys,emdm.fa.ap1.oraclecloud.com,CX_23001",
	"cherokeenationbusinesses,ejvp.fa.us2.oraclecloud.com,CX_4001",
	"creighton,hcps.fa.us2.oraclecloud.com,CX_1",
	"external,ekez.fa.em2.oraclecloud.com,CX",
	"levy,ejdf.fa.us6.oraclecloud.com,CX",
	"businessoperationsandlegal,ehpy.fa.em5.oraclecloud.com,CX_1001",
	"hmcexternal,fa-evlb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"caesarssportsbook,edmn.fa.us2.oraclecloud.com,CX_6001",
	"husurasivusto,ejnv.fa.em2.oraclecloud.com,CX_1",
	"aidbexternal,ibaeqy.fa.ocs.oraclecloud.com,CX_1",
	"definity,hdks.fa.ca2.oraclecloud.com,CX_1",
	"dubaiholding,esbe.fa.em8.oraclecloud.com,CX_1001",
	"trillium,iadyup.fa.ocs.oraclecloud.com,CX_4001",
	"wellenterprises,hcsb.fa.us2.oraclecloud.com,CX_1",
	"cyncly,fa-ewdg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"adibcandidateexperience,hciq.fa.em2.oraclecloud.com,CX_1",
	"rice,emdz.fa.us2.oraclecloud.com,CX_2001",
	"rcsd,fa-euum-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"newrivervalleycommunityservicescenter,fa-euwx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"salma,fa-eutv-saasfaprod1.fa.ocs.oraclecloud.com,CX_8001",
	"schrodersreferral,ekbq.fa.em2.oraclecloud.com,CX_1001",
	"eclerxonshore,fa-ewji-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"hblpeople,hdcs.fa.ap1.oraclecloud.com,CX_13009",
	"portsandlogistics,eibd.fa.em2.oraclecloud.com,CX_2021",
	"tennesseetech,fa-eygi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"foundationmedicine,hcyc.fa.us2.oraclecloud.com,CX_1",
	"novotechchina,fa-euzi-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"hollywoodbets,iagjme.fa.ocs.oraclecloud.com,CX_1005",
	"cityofgreeley,elvp.fa.us2.oraclecloud.com,CX_1001",
	"structuretoneunitedstates,fa-exrr-saasfaprod1.fa.ocs.oraclecloud.com,CX_5",
	"healthshieldmedicalcenter,eftr.fa.em2.oraclecloud.com,CX_1",
	"savola,fa-ewxl-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"awg,fa-etwq-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"cimbsingapore,ejox.fa.ap1.oraclecloud.com,CX_6",
	"resideo,ehtl.fa.us6.oraclecloud.com,CX",
	"anglophoneschooldistrict,emgi.fa.ca3.oraclecloud.com,CX_3001",
	"cmhacandidateexperience,enqn.fa.us2.oraclecloud.com,CX_1002",
	"southwestmississippiregionalmedicalcente,fa-evlp-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"kpmgdeliverynetworkindia1,ejgk.fa.em2.oraclecloud.com,CX_3001",
	"carringtonmortgage,emvo.fa.us2.oraclecloud.com,CX_3002",
	"southlanarkshirecouncil,fa-euuc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"goldengoose,iaagmj.fa.ocs.oraclecloud.com,CX_1",
	"omegaus,fa-equm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"greateagleofcompanies,fa-eqgp-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"gkn,fa-eryf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"omicron,omicron-iadzgs.fa.ocs.oraclecloud.com,CX_1",
	"austinpeay,ibqzjb.fa.ocs.oraclecloud.com,CX_1001",
	"universityofedinburgh,elxw.fa.em3.oraclecloud.com,CX_1001",
	"coherentchina,hcwp.fa.us2.oraclecloud.com,CX_7013",
	"leicestershirecountycouncil,eism.fa.em2.oraclecloud.com,CX",
	"verint,fa-epcb-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"camden,ehap.fa.us2.oraclecloud.com,CX",
	"gruppoa2a,hdeh.fa.em3.oraclecloud.com,CX_1",
	"mattson,eidg.fa.us6.oraclecloud.com,CX_2",
	"falck,fa-expf-saasfaeuraprod1.fa.ocs.oraclecloud.com,CX_1001",
	"europemiddleeastafrica,fa-euxw-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"lucy,hdga.fa.em3.oraclecloud.com,CX_1",
	"hoyaglobal,fa-esta-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"unitedsurgicalpartnersinternational,eodr-dev5.fa.us2.oraclecloud.com,CX_5001",
	"tenetexecutivesearch,eodr.fa.us2.oraclecloud.com,CX_1004",
	"subaru,hcal.fa.us2.oraclecloud.com,CX_1001",
	"unfpa,estm.fa.em2.oraclecloud.com,CX_2003",
	"unwomen,estm.fa.em2.oraclecloud.com,CX_1001",
	"iaajtj,iaajtj.fa.ocs.oraclecloud.com,CX_1",
	"ecnf,ecnf.fa.us2.oraclecloud.com,CX",
	"arukbuniversity,iabwey.fa.ocs.oraclecloud.com,CX_3",
	"stonhard,hcwx.fa.us2.oraclecloud.com,CX_1",
	"bocc,fa-eqsg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"scottishgovernmentogdrecruitment,fa-evxn-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1001",
	"lazardprofessional,icbpjb.fa.ocs.oraclecloud.com,CX_1",
	"ascot,fa-emkq-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"clp,iabhtj.fa.ocs.oraclecloud.com,CX_1",
	"northwestmscc,emor.fa.us2.oraclecloud.com,CX_2001",
	"emeapostings,fa-ewgu-saasfaprod1.fa.ocs.oraclecloud.com,CX_5001",
	"manhattandasupportstaffopenings,fa-elzs-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"atlantis,fa-exnk-saasfaprod1.fa.ocs.oraclecloud.com,CareerSite",
	"guardian,fa-eqnr-saasfaprod1.fa.ocs.oraclecloud.com,CX_1020",
	"tpicompositesholdings,fa-elwc-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"ebet,ebet.fa.em3.oraclecloud.com,CX_1",
	"dll,iabcbn.fa.ocs.oraclecloud.com,CX_1",
	"gci,edqv.fa.us2.oraclecloud.com,CX",
	"arlingtoncountyva,fa-exkk-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"nrcnorcap,ekum.fa.em2.oraclecloud.com,CX_2019",
	"davidson,fa-exci-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"rustoleum,hcwx.fa.us2.oraclecloud.com,CX_15",
	"lazardstudent,icbpjb.fa.ocs.oraclecloud.com,CX_2",
	"pccexternalminimaltemplate,fa-enmi-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"betsoftware,iagjme.fa.ocs.oraclecloud.com,CX_1001",
	"emergentholdingssection,ejko.fa.us2.oraclecloud.com,CX_2",
	"norfolkcountycouncil,fa-eqie-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"unitedlex,ekkk.fa.us6.oraclecloud.com,CX_1",
	"jgc,fa-ewcd-saasfaprod1.fa.ocs.oraclecloud.com,CX_5001",
	"csx,fa-eowa-saasfaprod1.fa.ocs.oraclecloud.com,CSXCareers",
	"privatedestinations,eicl.fa.em5.oraclecloud.com,CX_6001",
	"braskemcarreira,epiw.fa.la1.oraclecloud.com,CX_1001",
	"riyadhschools,ejpi.fa.em2.oraclecloud.com,CX_5001",
	"homepage,hdhy.fa.em3.oraclecloud.com,CX_1",
	"parker,elje.fa.us2.oraclecloud.com,CX",
	"uobemployee,edzz.fa.em3.oraclecloud.com,CX_6001",
	"efg,fa-eqai-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"nhaohio,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_16004",
	"seaspan,hckz.fa.us2.oraclecloud.com,CX_1",
	"cityofatlanta,ehxr.fa.us2.oraclecloud.com,City-of-Atlanta-Careers",
	"gmfinancialio,fa-exvu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1003",
	"harrahsresortsoutherncalifornia,edmn.fa.us2.oraclecloud.com,CX_8001",
	"svkmsjvparekhinternationalschool,fa-elxu-test-saasfaprod1.fa.ocs.oraclecloud.com,CX_1024",
	"polkbocc,fa-eqpz-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"hct,iaavey.fa.ocs.oraclecloud.com,CX_1",
	"ocado,iahbme.fa.ocs.oraclecloud.com,CX_1",
	"burfordscandidateexperience,edyo.fa.us2.oraclecloud.com,CX_1",
	"wheaton,fa-eukq-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"adanienterpriselimited,eibd.fa.em2.oraclecloud.com,CX_4001",
	"westmidlandspolice,ecwz.fa.em3.oraclecloud.com,CX_1",
	"staffandfaculty,ehwy.fa.us2.oraclecloud.com,CX_1",
	"oxfordnanoporetechnologies,ejnh.fa.em2.oraclecloud.com,CX_1",
	"specialeducationteaching,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_30001",
	"americanhospital,fa-epvs-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"eolj,eolj.fa.us2.oraclecloud.com,CX_3001",
	"coherentphilippines,hcwp.fa.us2.oraclecloud.com,CX_10001",
	"eecol,eklm.fa.us2.oraclecloud.com,CX_1001",
	"trevi,hcqr.fa.em2.oraclecloud.com,CX_2005",
	"galadaribrothers,efcs.fa.em3.oraclecloud.com,CX_1",
	"bancoguayaquil,fa-ewnb-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"stolafstudent,fa-ewur-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"cytel,iblyjb.fa.ocs.oraclecloud.com,CX_2001",
	"fedcapfedcap,eckb.fa.us2.oraclecloud.com,CX_1010",
	"knowledgehub,fa-emsb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"metalsa,hcun.fa.us2.oraclecloud.com,CX_1",
	"kingranch,eeof.fa.us6.oraclecloud.com,CX_1",
	"pittsburgstate,ebyf.fa.us2.oraclecloud.com,CX_1001",
	"ugscandidateexperience,edyo.fa.us2.oraclecloud.com,CX_1001",
	"faepodsaasfaprod1,fa-epod-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"kawneer,hdnn.fa.us6.oraclecloud.com,CX_1",
	"americantowerglobal,hdsn.fa.us6.oraclecloud.com,CX_1",
	"inl,elhe.fa.us8.oraclecloud.com,pro",
	"midmarkcorporationopportunities,hcor.fa.us2.oraclecloud.com,CX_1",
	"haveringcandidateexperience,elfy.fa.em3.oraclecloud.com,CX",
	"trailking,hcre.fa.us2.oraclecloud.com,CX_1",
	"vyllahome,emvo.fa.us2.oraclecloud.com,CX_3005",
	"londonboroughofbarnet,fa-exnu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"auib,fa-eqdg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"accnewzealand,fa-ernw-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"floridablue,fa-etum-saasfaprod1.fa.ocs.oraclecloud.com,floridablue",
	"healthcaredistrictpalmbeach,fa-ewje-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"latam,fa-ewgu-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"messiahuniversity,messiah-ibvrjb.fa.ocs.oraclecloud.com,CX_2",
	"australiansuper,ejjl.fa.ap1.oraclecloud.com,CX_1",
	"altayer,hchx.fa.em2.oraclecloud.com,CX_1",
	"stoltnielsen,eclo.fa.em2.oraclecloud.com,CX_1",
	"tamakihealth,iaantz.fa.ocs.oraclecloud.com,CX_1",
	"kingscollege,effb.fa.em3.oraclecloud.com,CX_1",
	"milestone,fa-ewto-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"hunt,fa-eqcd-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"dcwater,elxb.fa.us2.oraclecloud.com,CX",
	"ajax,fa-exrr-saasfaprod1.fa.ocs.oraclecloud.com,CX_6",
	"candidateexperiencepolyglass,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1005",
	"fhh,fa-emmq-saasfaprod1.fa.ocs.oraclecloud.com,CX_6001",
	"waynecounty,emqz.fa.us2.oraclecloud.com,CX_1",
	"actionforchildren,fa-evrg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"moravian,ibtsjb.fa.ocs.oraclecloud.com,CX_2",
	"okc,fa-etyr-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"heriotwattuniversity,enzj.fa.em3.oraclecloud.com,CX",
	"actionforchildrenvolunteers,fa-evrg-saasfaprod1.fa.ocs.oraclecloud.com,CX_3",
	"mx,ehtc.fa.ca2.oraclecloud.com,CX_1",
	"uniakarriereseite,fa-evpg-saasfaeuraprod1.fa.ocs.oraclecloud.com,CX_1001",
	"deltadentalins,ejep.fa.us2.oraclecloud.com,CX_1",
	"travelport,ejzg.fa.us6.oraclecloud.com,CX_1",
	"alorica2,fa-euxw-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"apco,fa-evxv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"nottinghamcitycouncil,eism.fa.em2.oraclecloud.com,CX_1",
	"amp,fa-esow-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"triaorthopedics,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2009",
	"pulamalanai,fa-ewcy-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"faekzusaasfaprod1,fa-ekzu-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"cozenoconnor,hctq.fa.us2.oraclecloud.com,CX_1",
	"datavail,eifn.fa.us6.oraclecloud.com,CX_3001",
	"bedbathandbeyond,eklv.fa.ca3.oraclecloud.com,bedbathandbeyond",
	"crs,eipn.fa.us2.oraclecloud.com,CX_1",
	"ukri,fa-evzn-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1001",
	"juilliard,fa-eoqj-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"speechlanguagepathologist,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_35001",
	"nharaleighdurham,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_18007",
	"gowlingwlg,ehjc.fa.em2.oraclecloud.com,CX_17",
	"bxp,edxn.fa.us2.oraclecloud.com,CX_5001",
	"icertis,iaaviz.fa.ocs.oraclecloud.com,CX_1",
	"intertekcanada,hcog.fa.em2.oraclecloud.com,CX_2",
	"eieb,eieb.fa.us6.oraclecloud.com,CX_1001",
	"sunbeltcontrols,ekkt.fa.us2.oraclecloud.com,CX_4",
	"flagler,fa-ewbi-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"ctb,emsl.fa.us2.oraclecloud.com,CX_1001",
	"worthingtonsteel,cbdt.fa.us2.oraclecloud.com,WorthingtonSteelCareers",
	"nmcuaen,eiby.fa.em2.oraclecloud.com,CX_1001",
	"fordplatformarchitecture,efds.fa.em5.oraclecloud.com,CX_7001",
	"enbdrecruitmentteam,fa-evlo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"ajmanuniversityv2,iabeey.fa.ocs.oraclecloud.com,CX_1001",
	"emids,fa-eupt-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"gcaa,elon.fa.em8.oraclecloud.com,CX_1001",
	"nationalpen,eihb.fa.em3.oraclecloud.com,CX_1",
	"profource,eccs.fa.em2.oraclecloud.com,CX_5001",
	"nisshamedical,ejvr.fa.us6.oraclecloud.com,CX_2",
	"bdousacampus,ebqb.fa.us2.oraclecloud.com,CX_1001",
	"harmonic,egmn.fa.us2.oraclecloud.com,CX_1",
	"grupoedsonqueirozv4,fa-eqbh-saasfaprod1.fa.ocs.oraclecloud.com,CX_7001",
	"eoja,eoja.fa.ap1.oraclecloud.com,CX",
	"governmentofnewbrunswick,emgi.fa.ca3.oraclecloud.com,CX_1001",
	"boutique,fa-eute-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"atmyersandstauffer,ebez.fa.us2.oraclecloud.com,CX_4",
	"saintmaryscollege,fa-eutm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"jobopportunitiesstaffmanagement,ehnn.fa.us2.oraclecloud.com,CX_4",
	"innergex,fa-ewqm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"valaris,efqq.fa.us6.oraclecloud.com,CX_1008",
	"icgsharedservices,hcwx.fa.us2.oraclecloud.com,CX_7",
	"svkmsschooldhule,fa-elxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_3005",
	"cityofgrandjunction,iascqy.fa.ocs.oraclecloud.com,CX_1",
	"firstsolarindia,fa-esbv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"horizonfarmcredit,eozs.fa.us6.oraclecloud.com,CX_1001",
	"duracell,fa-ewub-saasfaprod1.fa.ocs.oraclecloud.com,CX_26",
	"renewableenergy,eibd.fa.em2.oraclecloud.com,CX_2027",
	"savannahrivernationallaboratory,ewvl.fa.us8.oraclecloud.com,CX_1",
	"intermass,iaaley.fa.ocs.oraclecloud.com,CX_1",
	"siga,iaayzv.fa.ocs.oraclecloud.com,CX_1",
	"trinitycollege,trincore-ibvxjb.fa.ocs.oraclecloud.com,CX_1",
	"bcbsm,ejko.fa.us2.oraclecloud.com,CX_3",
	"myriadgeneticscandidate,ekgn.fa.us6.oraclecloud.com,CX_2001",
	"fifco,ibmdjb.fa.ocs.oraclecloud.com,CX_1",
	"egpa,egpa.fa.em3.oraclecloud.com,CX_1001",
	"minnesotajudicialbranch,fa-exco-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"mothersonnorthamericamx,hcnh.fa.us2.oraclecloud.com,CX_4001",
	"a49,emit.fa.ca3.oraclecloud.com,CX_1004",
	"cityofchattanooga,fa-eqto-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"minto,fa-erjt-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"dallahrecruitmentsystem,eniy.fa.em3.oraclecloud.com,CX_6001",
	"cityofmemphis,eeim.fa.us2.oraclecloud.com,CX",
	"thurrockcouncil,egst.fa.em3.oraclecloud.com,CX",
	"nhabrooklyn,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_18001",
	"ttx,ejjc.fa.us6.oraclecloud.com,CX",
	"solvias,iabxas.fa.ocs.oraclecloud.com,CX_1",
	"tatasteeluk,ialime.fa.ocs.oraclecloud.com,jobsattatasteeluk",
	"kldiscovery,ekug.fa.us6.oraclecloud.com,CX_1001",
	"coherentdeutschland,hcwp.fa.us2.oraclecloud.com,CX_3004",
	"alleghenycollege,alleghenycollege-ibwwjb.fa.ocs.oraclecloud.com,CX_2",
	"petroservicelimited,hdrc.fa.ca3.oraclecloud.com,CX_2002",
	"hayleysadvantis,emdm.fa.ap1.oraclecloud.com,CX_15009",
	"arcorglobal,emqm.fa.us6.oraclecloud.com,CX_2001",
	"phoenix,egeg.fa.em3.oraclecloud.com,CX_1",
	"partnerpharmacynew,fa-eqgc-saasfaprod1.fa.ocs.oraclecloud.com,CX_6",
	"silverfernfarmsexternal2022,fa-esmz-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"hispasat,iahtgs.fa.ocs.oraclecloud.com,CX_1",
	"miralexperiences,enpk.fa.em8.oraclecloud.com,CX_4001",
	"svkmsmukeshbhairpatelcbseschool,fa-elxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1036",
	"battenuniversity,fa-ewic-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"howmetaerospaceintern,fa-exty-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"skidmore,eodq.fa.us6.oraclecloud.com,CX",
	"mycronicjobopenings,ehtv.fa.em2.oraclecloud.com,CX_1",
	"newnahdi,efan.fa.em3.oraclecloud.com,CX_7001",
	"standardlife,fa-enor-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"hpmsthcexternal,fa-evlb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"mn8unitedstatesexternal,icczjb.fa.ocs.oraclecloud.com,CX_1",
	"caesarssouthernindiana,fa-eunv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"blackmorenew,hcpo.fa.ap1.oraclecloud.com,CX_2001",
	"dordtstudent,ibmxjb.fa.ocs.oraclecloud.com,CX_1",
	"hearsthealth,eevd.fa.us6.oraclecloud.com,CX_2",
	"kcb,eoin.fa.em3.oraclecloud.com,CX_3001",
	"sidramedicinejoblistings,fa-epxn-saasfaprod1.fa.ocs.oraclecloud.com,CX_7001",
	"svkmschatrabhujnarseememorialschoolndpar,fa-elxu-test-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"southafricanreservebank,fa-evra-saasfaprod1.fa.ocs.oraclecloud.com,CX_1002",
	"hutchinsonhealth,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2007",
	"bw1,fa-evup-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"sitoadr,iacrgs.fa.ocs.oraclecloud.com,CX_1",
	"oro,fa-evlf-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"agrifoods,ibanqy.fa.ocs.oraclecloud.com,CX_4",
	"massport,iapiqy.fa.ocs.oraclecloud.com,CX_1",
	"freedompointeatvillages,eexs.fa.us2.oraclecloud.com,CX_12093",
	"penskefrench,fa-euyk-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"postenogbring,fa-evem-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"onityexternal,ebhz.fa.us2.oraclecloud.com,CX_3003",
	"corserv,ekzo.fa.em2.oraclecloud.com,CX",
	"occupationaltherapist,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_36001",
	"saintmaryscollegestudenthiring,fa-eutm-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"cornwallcounciljobsand,ehgv.fa.em2.oraclecloud.com,CX_2",
	"lbwf,fa-evng-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"newcourtesyposting,evqk.fa.us8.oraclecloud.com,CX_3001",
	"novasystems,epdj.fa.ap1.oraclecloud.com,CX",
	"walsallcouncil,ejti.fa.em3.oraclecloud.com,CX_1",
	"suffolkcountycouncilinternaljobsboard,eoce.fa.em3.oraclecloud.com,CX_4001",
	"faemadsaasfaprod1,fa-emad-saasfaprod1.fa.ocs.oraclecloud.com,CX_6001",
	"casadelascampanas,eexs.fa.us2.oraclecloud.com,CX_14023",
	"wagner,fa-exad-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"apprecruitment,eify.fa.us6.oraclecloud.com,CX_1001",
	"nhacentralmichigan,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_13001",
	"socialworker,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_36004",
	"tatateleservicesportal,fa-evmm-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"unisuper,fa-eugn-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"modernmachinery,fa-evwo-saasfaprod1.fa.ocs.oraclecloud.com,CX_3",
	"freedomvillageatbrandywine,eexs.fa.us2.oraclecloud.com,CX_12081",
	"outwoodgrangeacadamiestrust,fa-eqvg-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"scottishgovernmentrecruitment,fa-evxn-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1",
	"cityofhendersonville,emmr.fa.us2.oraclecloud.com,CX",
	"ib,ejst.fa.em3.oraclecloud.com,CX_1",
	"psychologist,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_35004",
	"pemco,ejkz.fa.us2.oraclecloud.com,CX_1",
	"atbutler,fa-exer-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"stolafcollege,fa-ewur-saasfaprod1.fa.ocs.oraclecloud.com,CX_3",
	"itv,fa-euup-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"universityofwaikato,elhs.fa.ap1.oraclecloud.com,CX",
	"faenrusaasfaprod1,fa-enru-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"universityofwollongong,ejgl.fa.ap1.oraclecloud.com,CX_1",
	"yumindiaglobalservices,eczd.fa.us2.oraclecloud.com,CX_17021",
	"hudsonhospital,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"marta,fa-evii-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"insead,iablgs.fa.ocs.oraclecloud.com,CX_1",
	"envisionhealthcare,eney.fa.us2.oraclecloud.com,CX_1001",
	"atheathrow,encd.fa.em3.oraclecloud.com,CX_6005",
	"abdullafouadminimal,hdgp.fa.em3.oraclecloud.com,CX_2001",
	"sbc,egsd.fa.us2.oraclecloud.com,CX",
	"airasiaindia,fa-eron-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"damac,ejwe.fa.em2.oraclecloud.com,CX_1",
	"airtelafrica,erey.fa.em3.oraclecloud.com,CX_1",
	"friendshipvillageoftempe,eexs.fa.us2.oraclecloud.com,CX_12029",
	"sedgebrook,eexs.fa.us2.oraclecloud.com,CX_10013",
	"forumatranchohealthcenter,eexs.fa.us2.oraclecloud.com,CX_12047",
	"sagewood,eexs.fa.us2.oraclecloud.com,CX_11017",
	"aspetarcandidateexperience,eipb.fa.em2.oraclecloud.com,CX_5",
	"prioritymoversportal,fa-evxn-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_2001",
	"agsouthfarmcredit,eozs.fa.us6.oraclecloud.com,CX_13",
	"relationshipmanagers,hdcs.fa.ap1.oraclecloud.com,CX_13004",
	"branchmanagers,hdcs.fa.ap1.oraclecloud.com,CX_13003",
	"lewisham,efuy.fa.em3.oraclecloud.com,CX_1",
	"roadsmetrorailandwaterrmrw,eibd.fa.em2.oraclecloud.com,CX_2029",
	"talentodospinos,fa-enfw-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"ethekwini,iabzbn.fa.ocs.oraclecloud.com,CX_1",
	"cypressvillage,eexs.fa.us2.oraclecloud.com,CX_12085",
	"strathconacounty,fa-erjf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"mhra,eckx.fa.em2.oraclecloud.com,CX_1002",
	"cimbcambodia,ejox.fa.ap1.oraclecloud.com,CX_2",
	"cashofficers,hdcs.fa.ap1.oraclecloud.com,CX_13008",
	"westfieldshospitalclinic,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2005",
	"winningform,iagjme.fa.ocs.oraclecloud.com,CX_1",
	"peoplefirstbank,hcyt.fa.ap1.oraclecloud.com,CX",
	"groupama,eocd.fa.em2.oraclecloud.com,CX_1001",
	"plus,ennq.fa.ap1.oraclecloud.com,CX_2",
	"copa,ejom.fa.us6.oraclecloud.com,CX_1",
	"thurstoncounty,fa-etsa-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"phgtoe,fa-eutv-saasfaprod1.fa.ocs.oraclecloud.com,CX_6007",
	"lakeportsquare,eexs.fa.us2.oraclecloud.com,CX_12073",
	"moldawresidences,eexs.fa.us2.oraclecloud.com,CX_12023",
	"abtglobal,egpy.fa.us2.oraclecloud.com,CX_3001",
	"zayeduniversity,fa-evge-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"accutech,eklm.fa.us2.oraclecloud.com,CX_1",
	"agfirstfarmcreditbank,eozs.fa.us6.oraclecloud.com,CX_1",
	"capmetrocorporate,fa-eujk-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"reale,hcqt.fa.em2.oraclecloud.com,CX_1",
	"swan,epin.fa.em2.oraclecloud.com,CX_1",
	"parisima,fa-ewmw-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"uoestudentjobs,elxw.fa.em3.oraclecloud.com,CX_1004",
	"marshesofskidawayisland,eexs.fa.us2.oraclecloud.com,CX_14035",
	"friendshipvillagekalamazoo,eexs.fa.us2.oraclecloud.com,CX_14027",
	"depaultalentacquisition,ekze.fa.us2.oraclecloud.com,CX_1001",
	"universityofsalford,iahgme.fa.ocs.oraclecloud.com,CX_3001",
	"setonhilluniversity,setonhill-ibtwjb.fa.ocs.oraclecloud.com,CX_2",
	"carboline,hcwx.fa.us2.oraclecloud.com,CX_12",
	"farmcreditbankoftexas,eozs.fa.us6.oraclecloud.com,CX_6",
	"milaha,ejqa.fa.em2.oraclecloud.com,CX_4",
	"londonboroughofbrent,fa-epzg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"alothmanholding,eknh.fa.em2.oraclecloud.com,CX_6001",
	"ifminvestors,enlc.fa.ap1.oraclecloud.com,CX_1001",
	"atpeoplescoutpeoplescoutjobs,ehnn.fa.us2.oraclecloud.com,CX_1001",
	"esecexperience,ebfi.fa.em2.oraclecloud.com,CX_1001",
	"sitiodecarreralaureate,fa-evib-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"awr,iacpey.fa.ocs.oraclecloud.com,CX_1",
	"universityoftulsastudent,utulsa-ibvjjb.fa.ocs.oraclecloud.com,CX_2",
	"tennischannel,edyy.fa.us2.oraclecloud.com,CX_6001",
	"emarat,fa-expo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"monarchlanding,eexs.fa.us2.oraclecloud.com,CX_10015",
	"freedomsquare,eexs.fa.us2.oraclecloud.com,CX_6001",
	"bellinghamatwestchester,eexs.fa.us2.oraclecloud.com,CX_26007",
	"spring,fa-evrg-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"coherentswitzerland,hcwp.fa.us2.oraclecloud.com,CX_4001",
	"coherentsweden,hcwp.fa.us2.oraclecloud.com,CX_7012",
	"kpmgfinland,iabdgs.fa.ocs.oraclecloud.com,CX_1002",
	"oliviahospitalclinic,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_7",
	"newapplicants,ebtw.fa.us2.oraclecloud.com,CX_1",
	"sunflower,ecvu.fa.us2.oraclecloud.com,CX",
	"servicecenter,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_29024",
	"lavoraconnoigruppocredem,hczf.fa.em2.oraclecloud.com,CX_1",
	"purefusion,fa-euii-saasfaprod1.fa.ocs.oraclecloud.com,CX_11001",
	"nmdp,hddl.fa.us2.oraclecloud.com,CX_2",
	"australiancountrychoice,fa-eslq-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"webercounty,fa-etrb-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"ehcpuertorico,ibwsjb.fa.ocs.oraclecloud.com,CX_1004",
	"mtnexternal,ehle.fa.em2.oraclecloud.com,CX_1",
	"lcs,eexs.fa.us2.oraclecloud.com,CX_1",
	"heritageatbrentwood,eexs.fa.us2.oraclecloud.com,CX_11015",
	"greenwoodvillagesouth,eexs.fa.us2.oraclecloud.com,CX_14015",
	"roseseniorlivingavon,eexs.fa.us2.oraclecloud.com,CX_14021",
	"clare,eexs.fa.us2.oraclecloud.com,CX_8003",
	"essexmeadows,eexs.fa.us2.oraclecloud.com,CX_9001",
	"lauralparke,eexs.fa.us2.oraclecloud.com,CX_35005",
	"galleriawoods,eexs.fa.us2.oraclecloud.com,CX_12089",
	"grantthorntonnorthernireland,ehzq.fa.us2.oraclecloud.com,CX_2004",
	"coherentindia,hcwp.fa.us2.oraclecloud.com,CX_8001",
	"healthpartnersclinicstillwater,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"nhaindiana,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_16001",
	"ash,fa-euud-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"delegat,eoiq.fa.ap1.oraclecloud.com,CX",
	"stcroixcountysection,efff.fa.us6.oraclecloud.com,CX_1",
	"wavelifesciences,ectf.fa.us2.oraclecloud.com,CX_1",
	"msdstlouis,fa-eudi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"icrossing,eevd.fa.us6.oraclecloud.com,CX_18",
	"genesis,hcxg.fa.em2.oraclecloud.com,CX_2",
	"timberridgeattalus,eexs.fa.us2.oraclecloud.com,CX_10017",
	"cedarsofchapelhill,eexs.fa.us2.oraclecloud.com,CX_14033",
	"cypressofcharlotte,eexs.fa.us2.oraclecloud.com,CX_14013",
	"tcipowdercoatings,hcwx.fa.us2.oraclecloud.com,CX_14",
	"ajmanuniversityacademicsupport,iabeey.fa.ocs.oraclecloud.com,CX_2001",
	"nhcinnovation,eghj.fa.em2.oraclecloud.com,CX_6001",
	"cncpoliceofficer,fa-euhj-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1001",
	"inlandrevenue,ekfu.fa.ap1.oraclecloud.com,CX_1001",
	"synlaitexternal,ekvx.fa.ap1.oraclecloud.com,CX_1001",
	"schoolleadershipandofficeadministration,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_31001",
	"mvcc,fa-euyo-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"sumitomoshifw,eiox.fa.em2.oraclecloud.com,CX_1",
	"suffolkcountycouncilredeploymentjobs,eoce.fa.em3.oraclecloud.com,CX_3007",
	"qatarsteel,fa-ewab-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"grupomoura,fa-ewzu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"southyorkshirepolice,fa-exru-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_1",
	"aubmc,fa-exxn-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"executive,hcxs.fa.us2.oraclecloud.com,CX_1001",
	"hnpctmedia,eevd.fa.us6.oraclecloud.com,CX_11004",
	"hnpdallasmorningnews,eevd.fa.us6.oraclecloud.com,CX_15001",
	"freedomvillagebradenton,eexs.fa.us2.oraclecloud.com,CX_12095",
	"brandonwilde,eexs.fa.us2.oraclecloud.com,CX_8005",
	"blakehurst,eexs.fa.us2.oraclecloud.com,CX_10033",
	"delaneyatvale,eexs.fa.us2.oraclecloud.com,CX_10001",
	"rollinggreenvillage,eexs.fa.us2.oraclecloud.com,CX_14011",
	"carillon,eexs.fa.us2.oraclecloud.com,CX_15017",
	"euclidchemical,hcwx.fa.us2.oraclecloud.com,CX_5",
	"lbcorc,ecum.fa.em2.oraclecloud.com,CX_2",
	"elcavietnam,iaaras.fa.ocs.oraclecloud.com,CX_3",
	"coherentuk,hcwp.fa.us2.oraclecloud.com,CX_2001",
	"govanbrown,fa-exrr-dev2-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"genex,hdjm.fa.ca2.oraclecloud.com,CX_2",
	"hccportal,hcre.fa.us2.oraclecloud.com,CX_3001",
	"otsukaicumed,eduu.fa.us2.oraclecloud.com,CX_1001",
	"smithmechanicalelectricplumbing,ekkt.fa.us2.oraclecloud.com,CX_4001",
	"nhawisconsin,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_15001",
	"nhawinterville,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_18010",
	"logixhealth,fa-exql-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"namossolutions,edfl.fa.em2.oraclecloud.com,CX_1001",
	"graduateandapprenticeship,ehpy.fa.em5.oraclecloud.com,CX_1004",
	"dbdef,fa-eozc-saasfaprod1.fa.ocs.oraclecloud.com,CX_7001",
	"okczoo,fa-etyr-saasfaprod1.fa.ocs.oraclecloud.com,CX_5",
	"southlanarkshireleisureandculturecouncil,fa-euuc-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"skmca,fa-exqb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1003",
	"eaglehills,iaauas.fa.ocs.oraclecloud.com,CX_1",
	"aiccinternal,iaarkf.fa.ocs.oraclecloud.com,CX_1001",
	"dordtuniversity,ibmxjb.fa.ocs.oraclecloud.com,CX_2",
	"telamon,hcex.fa.us2.oraclecloud.com,CX_3",
	"summitfire,fa-exxm-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"hnphoustonchronicle,eevd.fa.us6.oraclecloud.com,CX_10007",
	"cottagegroveplace,eexs.fa.us2.oraclecloud.com,CX_12041",
	"regencyoaksclearwater,eexs.fa.us2.oraclecloud.com,CX_12077",
	"clarendaleatindianlakes,eexs.fa.us2.oraclecloud.com,CX_13005",
	"eastcasleplace,eexs.fa.us2.oraclecloud.com,CX_12051",
	"clarendaleofalgonquin,eexs.fa.us2.oraclecloud.com,CX_10037",
	"clarendalewestend,eexs.fa.us2.oraclecloud.com,CX_22005",
	"clarendalesixcorners,eexs.fa.us2.oraclecloud.com,CX_12019",
	"cleanharborsfr,epyc.fa.us2.oraclecloud.com,CX_5001",
	"grandturk,eicl.fa.em5.oraclecloud.com,CX_7001",
	"gmfinancialcanada,fa-exvu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1002",
	"ecobankprofessional,fa-emqf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1004",
	"legendbrands,hcwx.fa.us2.oraclecloud.com,CX_2",
	"enbdindiarecruitmentteam,fa-evlo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"cimbvietnam,ejox.fa.ap1.oraclecloud.com,CX_11",
	"btnsites,bankbtn-hcis-iaadnf.fa.ocs.oraclecloud.com,CX_2001",
	"canadianuniversitydubai,fa-eufk-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"lfdriscoll,fa-exrr-dev2-saasfaprod1.fa.ocs.oraclecloud.com,CX_7",
	"firstsolarvietnam,fa-esbv-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"mayouk,fa-euwp-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"adanitotalgasltd,eibd.fa.em2.oraclecloud.com,CX_2005",
	"vgi,ibzbjb.fa.ocs.oraclecloud.com,CX_6",
	"qf,fa-eolw-saasfaprod1.fa.ocs.oraclecloud.com,CX",
	"lucyheadoffice,hdga.fa.em3.oraclecloud.com,CX_9",
	"arcorbrasil,emqm.fa.us6.oraclecloud.com,CX_1001",
	"eastsuffolkcouncilinternaljobs,eoce.fa.em3.oraclecloud.com,CX_13001",
	"daman,erel.fa.em8.oraclecloud.com,CX_1001",
	"advancedpharmacysolutionsnew,fa-eqgc-saasfaprod1.fa.ocs.oraclecloud.com,CX_9",
	"vmiaexternal,iaacnf.fa.ocs.oraclecloud.com,CX_1",
	"citb,iagvme.fa.ocs.oraclecloud.com,CX_1",
	"hnpmidwestcommunities,eevd.fa.us6.oraclecloud.com,CX_11001",
	"portersneckvillage,eexs.fa.us2.oraclecloud.com,CX_12035",
	"wyndemere,eexs.fa.us2.oraclecloud.com,CX_5001",
	"marquette,eexs.fa.us2.oraclecloud.com,CX_12033",
	"hearthwoodseniorliving,eexs.fa.us2.oraclecloud.com,CX_28005",
	"greenhills,eexs.fa.us2.oraclecloud.com,CX_1001",
	"roseseniorlivingcarmel,eexs.fa.us2.oraclecloud.com,CX_15009",
	"fibergratecompositestructures,hcwx.fa.us2.oraclecloud.com,CX_9",
	"englandrugbycasual,ecer.fa.em2.oraclecloud.com,CX_1001",
	"svkmsschooljadcherla,fa-elxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"oceancapitalholdings,hdrc.fa.ca3.oraclecloud.com,CX_4001",
	"lewishamschools,efuy.fa.em3.oraclecloud.com,CX_5002",
	"kfc,eczd.fa.us2.oraclecloud.com,CX_4001",
	"healthpartnersriverwayclinics,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2015",
	"ameryhospitalclinic,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2003",
	"musiccitycenter,ibqhjb.fa.ocs.oraclecloud.com,CX_1003",
	"cherokeenationbusinessescorporate,ejvp.fa.us2.oraclecloud.com,CX_1",
	"nhaupstatenewyork,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_17001",
	"embark,fa-etyr-saasfaprod1.fa.ocs.oraclecloud.com,CX_2003",
	"saskatchewanwcbsaskatchewanworkerscompen,fa-ewle-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"sepaq,fa-ewph-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"vslrenewable,iabqiz.fa.ocs.oraclecloud.com,CX_5001",
	"acc,ibyrjb.fa.ocs.oraclecloud.com,CX_1",
	"elpasoelectric,ibrvjb.fa.ocs.oraclecloud.com,CX_1",
	"wmo,estm.fa.em2.oraclecloud.com,CX_5001",
	"envirocon,fa-evwo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"hearstcorporate,eevd.fa.us6.oraclecloud.com,CX_7",
	"localedge,eevd.fa.us6.oraclecloud.com,CX_14",
	"hnptexascommunities,eevd.fa.us6.oraclecloud.com,CX_10004",
	"richmondplaceseniorliving,eexs.fa.us2.oraclecloud.com,CX_12071",
	"lakeseminolesquare,eexs.fa.us2.oraclecloud.com,CX_12079",
	"freedomplazasuncitycenter,eexs.fa.us2.oraclecloud.com,CX_12087",
	"greencountryvillage,eexs.fa.us2.oraclecloud.com,CX_12043",
	"residencesatvantagepoint,eexs.fa.us2.oraclecloud.com,CX_15005",
	"masonicvillageatburlington,eexs.fa.us2.oraclecloud.com,CX_15015",
	"avalonofbloomfieldtownship,eexs.fa.us2.oraclecloud.com,CX_16013",
	"roseseniorlivingatprovidencepark,eexs.fa.us2.oraclecloud.com,CX_15013",
	"broadwaycityview,eexs.fa.us2.oraclecloud.com,CX_26005",
	"rslfarmingtonhills,eexs.fa.us2.oraclecloud.com,CX_21005",
	"academycandidateexperience,eipb.fa.em2.oraclecloud.com,CX_1",
	"emirateislamicbankrecruitmentteam,fa-evlo-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"rcandersen,fa-exrr-saasfaprod1.fa.ocs.oraclecloud.com,CX_9",
	"legacylandbankflca,eozs.fa.us6.oraclecloud.com,CX_26",
	"fedcapgranitepathways,eckb.fa.us2.oraclecloud.com,CX_1011",
	"multiconsult,multiconsult-group-iacabn.fa.ocs.oraclecloud.com,CX_1",
	"esmvacancies,ebuz.fa.em2.oraclecloud.com,CX",
	"c2italentacquisitionteam,fa-ewnp-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"seadrillcandidateexperience,fa-eykz-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"da,hcrg.fa.em2.oraclecloud.com,CX_1",
	"vslrepower,iabqiz.fa.ocs.oraclecloud.com,CX_3001",
	"irenaemployment,eexh.fa.em3.oraclecloud.com,CX_1",
	"fidelity,fa-eqva-saasfaprod1.fa.ocs.oraclecloud.com,FidelityCareerSite",
	"early,eedu.fa.em3.oraclecloud.com,CX_3001",
	"montanaresources,fa-evwo-saasfaprod1.fa.ocs.oraclecloud.com,CX_5",
	"hnpalbanytimes,eevd.fa.us6.oraclecloud.com,CX_10001",
	"cdsglobal,eevd.fa.us6.oraclecloud.com,CX_8",
	"candidateexperienceasia,hdhe.fa.em3.oraclecloud.com,CX_1",
	"emaar,emhm.fa.em2.oraclecloud.com,CX_1001",
	"glenviewatpelicanbay,eexs.fa.us2.oraclecloud.com,CX_12061",
	"arlingtonofnaples,eexs.fa.us2.oraclecloud.com,CX_11013",
	"oakmontgardens,eexs.fa.us2.oraclecloud.com,CX_10005",
	"heritageatirenewoods,eexs.fa.us2.oraclecloud.com,CX_14019",
	"emersonatstpeters,eexs.fa.us2.oraclecloud.com,CX_27007",
	"havenwoodofburnsville,eexs.fa.us2.oraclecloud.com,CX_17005",
	"trilliumwoods,eexs.fa.us2.oraclecloud.com,CX_11019",
	"clarendalearcadia,eexs.fa.us2.oraclecloud.com,CX_12017",
	"roseseniorlivingbeachwood,eexs.fa.us2.oraclecloud.com,CX_15007",
	"havenwoodofmaplegrove,eexs.fa.us2.oraclecloud.com,CX_16009",
	"englandrugby,ecer.fa.em2.oraclecloud.com,CX_1",
	"svkmsmukeshbhairpatelmilitaryschooljunio,fa-elxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_6001",
	"otherretailbankingjobs,hdcs.fa.ap1.oraclecloud.com,CX_13005",
	"acadiabroadcasting,hdrc.fa.ca3.oraclecloud.com,CX_1",
	"pulse,ejgl.fa.ap1.oraclecloud.com,CX_2001",
	"mbc,ehff.fa.em2.oraclecloud.com,CX_2001",
	"summitfirenationalconsulting,fa-exxm-saasfaprod1.fa.ocs.oraclecloud.com,CX_3",
	"tgwfsjobs,iaboey.fa.ocs.oraclecloud.com,CX_3",
	"fedcapmvle,eckb.fa.us2.oraclecloud.com,CX_1021",
	"nhaconcordia,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_21001",
	"skygen,fa-eqhn-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"abudhabiairports,hcts.fa.em2.oraclecloud.com,CX_1",
	"wmfs,fa-ertg-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"fluidmaster,fa-exwc-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"alghurairexternalemiratinationals,iaaxey.fa.ocs.oraclecloud.com,CX_1001",
	"localtruckdrivingjobscenterlinedrivers,ehnn.fa.us2.oraclecloud.com,CX_10",
	"renewableworks,ehnn.fa.us2.oraclecloud.com,CX_2001",
	"monrovia,ejbi.fa.us2.oraclecloud.com,CX",
	"empleosicbc,ejig.fa.em2.oraclecloud.com,CX_1",
	"scottbaderglobal,fa-eqzr-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"vikramsolar,iabqiz.fa.ocs.oraclecloud.com,CX_1",
	"reiteraffiliatedcompanies,eohm.fa.us2.oraclecloud.com,CX_6004",
	"hapocommunitycreditunion,hckg.fa.us2.oraclecloud.com,CX_1",
	"ncga,eoee.fa.us6.oraclecloud.com,CX_1001",
	"portofdover,iagxme.fa.ocs.oraclecloud.com,CX_1",
	"hnpsanfranciscobayarea,eevd.fa.us6.oraclecloud.com,CX_11007",
	"studentcasual,edzz.fa.em3.oraclecloud.com,CX_6004",
	"avalonofcommercetownship,eexs.fa.us2.oraclecloud.com,CX_17011",
	"clarendaleofchandler,eexs.fa.us2.oraclecloud.com,CX_12009",
	"avalonofnewalbany,eexs.fa.us2.oraclecloud.com,CX_17009",
	"harbourvillage,eexs.fa.us2.oraclecloud.com,CX_12067",
	"sterlingestatesofwestcobb,eexs.fa.us2.oraclecloud.com,CX_34005",
	"roseseniorlivingclintontownship,eexs.fa.us2.oraclecloud.com,CX_15011",
	"clarendaleannarbor,eexs.fa.us2.oraclecloud.com,CX_30007",
	"northpointewoods,eexs.fa.us2.oraclecloud.com,CX_12045",
	"va,iagime.fa.ocs.oraclecloud.com,CX_1",
	"csmart,eicl.fa.em5.oraclecloud.com,CX_2001",
	"dayglo,hcwx.fa.us2.oraclecloud.com,CX_13",
	"mantrose,hcwx.fa.us2.oraclecloud.com,CX_19",
	"rpminternational,hcwx.fa.us2.oraclecloud.com,CX_8",
	"grantthorntonisleofman,ehzq.fa.us2.oraclecloud.com,CX_3007",
	"physdnp,eppr.fa.us2.oraclecloud.com,CX_1001",
	"naturescot,ejki.fa.em3.oraclecloud.com,CX",
	"coherentthailand,hcwp.fa.us2.oraclecloud.com,CX_9001",
	"pavarinimcgovern,fa-exrr-dev2-saasfaprod1.fa.ocs.oraclecloud.com,CX_12",
	"du,fa-ewnx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"fedcapuk,eckb.fa.us2.oraclecloud.com,CX_1018",
	"fedcapeastersealsrhodeisland,eckb.fa.us2.oraclecloud.com,CX_1006",
	"capitolviewtransitionalcarecenter,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"magis,fa-ewci-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"ppecb,iaaqbn.fa.ocs.oraclecloud.com,CX_1",
	"etihadrail,etihadrail-iaalbv.fa.ocs.oraclecloud.com,CX_1",
	"banglalink,fa-esth-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"vodafoneqatar,elat.fa.em2.oraclecloud.com,CX",
	"webercountysheriff,fa-etrb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"thc,fa-euok-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"msa,fa-exdf-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"structuretoneinternational,fa-exrr-saasfaprod1.fa.ocs.oraclecloud.com,CX_10",
	"precisionconstruction,efsp.fa.us6.oraclecloud.com,CX_1001",
	"hoffmanspecialtycontracting,efsp.fa.us6.oraclecloud.com,CX_4",
	"grupowerthein,egky.fa.us6.oraclecloud.com,CX_5001",
	"delaneyatgeorgetown,eexs.fa.us2.oraclecloud.com,CX_10009",
	"candidateexperienceforinterns,edel.fa.us2.oraclecloud.com,CX_1001",
	"hearsttransportation,eevd.fa.us6.oraclecloud.com,CX_12",
	"hnpsanantonio,eevd.fa.us6.oraclecloud.com,CX_11010",
	"dielectric,edyy.fa.us2.oraclecloud.com,CX_5001",
	"digitalremedy,edyy.fa.us2.oraclecloud.com,CX_13015",
	"aloricacanada,fa-euxw-saasfaprod1.fa.ocs.oraclecloud.com,CX_5001",
	"hdbc,hdbc.fa.em2.oraclecloud.com,CX",
	"danberryatinverness,eexs.fa.us2.oraclecloud.com,CX_10031",
	"windsoratcelebration,eexs.fa.us2.oraclecloud.com,CX_12053",
	"delaneyatgreen,eexs.fa.us2.oraclecloud.com,CX_19005",
	"clarendaleclayton,eexs.fa.us2.oraclecloud.com,CX_12015",
	"millcroft,eexs.fa.us2.oraclecloud.com,CX_14005",
	"havenwoodofrichfield,eexs.fa.us2.oraclecloud.com,CX_16005",
	"havenwoodofbuffalo,eexs.fa.us2.oraclecloud.com,CX_17007",
	"rooseveltatsaltcreek,eexs.fa.us2.oraclecloud.com,CX_20005",
	"bayshoreonhiltonhead,eexs.fa.us2.oraclecloud.com,CX_12059",
	"havenwoodofminnetonka,eexs.fa.us2.oraclecloud.com,CX_16007",
	"saudiairnavigationservices,fa-esti-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"equityresidential,hcjm.fa.us2.oraclecloud.com,CX_1",
	"wagnerstudent,fa-exad-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"vitashealthcarecareerfair,ejrz.fa.us2.oraclecloud.com,CX_6001",
	"slabcareersite,efpb.fa.em3.oraclecloud.com,CX_1003",
	"bcci,fa-exrr-dev2-saasfaprod1.fa.ocs.oraclecloud.com,CX_3",
	"pavarini,fa-exrr-saasfaprod1.fa.ocs.oraclecloud.com,CX_11",
	"louisianalandbank,eozs.fa.us6.oraclecloud.com,CX_28",
	"farmcreditofflorida,eozs.fa.us6.oraclecloud.com,CX_34",
	"plainslandbankflca,eozs.fa.us6.oraclecloud.com,CX_8",
	"agcarolinafarmcredit,eozs.fa.us6.oraclecloud.com,CX_11",
	"leachmancattle,hdjm.fa.ca2.oraclecloud.com,CX_4001",
	"admntraining,emcw.fa.em8.oraclecloud.com,CX_3001",
	"hcpsevents,fa-exea-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"fedcapfedcaprehabilitation,eckb.fa.us2.oraclecloud.com,CX_1002",
	"acquisition,eguq.fa.us2.oraclecloud.com,CX_5001",
	"powertransmissionanddistribution,eibd.fa.em2.oraclecloud.com,CX_2023",
	"kingabdullahbinabdulazizuniversityhospit,fa-epxe-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"sharjahmaritimeacademy,fa-exow-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"umniah,iaavas.fa.ocs.oraclecloud.com,CX_4001",
	"leejamofficialnew,ebpy.fa.em2.oraclecloud.com,CX_1",
	"pccstandardminimaltemplate,fa-enmi-saasfaprod1.fa.ocs.oraclecloud.com,CX_1093",
	"officeadministration,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_29027",
	"nhavanderbilt,fa-eotc-saasfaprod1.fa.ocs.oraclecloud.com,CX_22001",
	"candidateexperiencevinavil,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"clevelandclinicabudhabi,fa-ewmp-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"simosinsourcingsolutions,ehnn.fa.us2.oraclecloud.com,CX_7",
	"teekay,eipv.fa.us6.oraclecloud.com,CX_2001",
	"jobopportunitiesmcb,ekbd.fa.em2.oraclecloud.com,CX",
	"airborneo,airborneo-iacatj.fa.ocs.oraclecloud.com,CX_1001",
	"aub,fa-exxn-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"vsllogistics,iabqiz.fa.ocs.oraclecloud.com,CX_2001",
	"epmsitioprivado,hbcp.fa.us2.oraclecloud.com,CX_7",
	"torneos,egky.fa.us6.oraclecloud.com,CX_6001",
	"epmsitio,hbcp.fa.us2.oraclecloud.com,CX_1",
	"dpworldinternship,ehpv.fa.em2.oraclecloud.com,CX_7001",
	"bluewaterrailservices,fa-evwo-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"zynxhealth,eevd.fa.us6.oraclecloud.com,CX_3",
	"oldemeacopy,fa-euxw-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"cypressofhiltonhead,eexs.fa.us2.oraclecloud.com,CX_12021",
	"villageatwoodlandsandwaterway,eexs.fa.us2.oraclecloud.com,CX_12063",
	"clarendaleatbellevueplace,eexs.fa.us2.oraclecloud.com,CX_12011",
	"sandhillcove,eexs.fa.us2.oraclecloud.com,CX_9003",
	"lodgeofnorthbrook,eexs.fa.us2.oraclecloud.com,CX_31007",
	"clarendaleofaddison,eexs.fa.us2.oraclecloud.com,CX_12007",
	"gablepines,eexs.fa.us2.oraclecloud.com,CX_12055",
	"avalonofauburnhills,eexs.fa.us2.oraclecloud.com,CX_16015",
	"clarendaleofstpeters,eexs.fa.us2.oraclecloud.com,CX_12013",
	"delaneyatsouthshore,eexs.fa.us2.oraclecloud.com,CX_11007",
	"sterlingestatesofeastcobb,eexs.fa.us2.oraclecloud.com,CX_34007",
	"delaneyofbridgewater,eexs.fa.us2.oraclecloud.com,CX_11011",
	"cypressofraleigh,eexs.fa.us2.oraclecloud.com,CX_12027",
	"kirker,hcwx.fa.us2.oraclecloud.com,CX_18",
	"cumminsskillbridgepartnership,fa-espx-saasfaprod1.fa.ocs.oraclecloud.com,CX_1003",
	"test,fa-evyu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1004",
	"cityofchattanoogapolice,fa-eqto-saasfaprod1.fa.ocs.oraclecloud.com,CX_1004",
	"coherenttaiwan,hcwp.fa.us2.oraclecloud.com,CX_7008",
	"coherentfinland,hcwp.fa.us2.oraclecloud.com,CX_11001",
	"edzt,edzt.fa.em4.oraclecloud.com,CX_1",
	"barneteducationandlearningservice,fa-exnu-saasfaprod1.fa.ocs.oraclecloud.com,CX_2",
	"firstsolarmalaysia,fa-esbv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"farmcreditofcentralflorida,eozs.fa.us6.oraclecloud.com,CX_21",
	"rivervalleyagcredit,eozs.fa.us6.oraclecloud.com,CX_30",
	"texasfarmcreditservices,eozs.fa.us6.oraclecloud.com,CX_9",
	"aggeorgiafarmcredit,eozs.fa.us6.oraclecloud.com,CX_14",
	"farmcreditofvirginias,eozs.fa.us6.oraclecloud.com,CX_24",
	"firstsouthfarmcreditaca,eozs.fa.us6.oraclecloud.com,CX_4",
	"southwestgeorgiafarmcredit,eozs.fa.us6.oraclecloud.com,CX_32",
	"islamicbanking,hdcs.fa.ap1.oraclecloud.com,CX_13002",
	"saskatooncolostrum,hdjm.fa.ca2.oraclecloud.com,CX_3001",
	"brunswickbrokers,hdrc.fa.ca3.oraclecloud.com,CX_8002",
	"emss,eism.fa.em2.oraclecloud.com,CX_1001",
	"sitoesperienzacandidati,eizj.fa.em2.oraclecloud.com,CX",
	"fedcapcommunityworkshops,eckb.fa.us2.oraclecloud.com,CX_1004",
	"habitburgergrill,eczd.fa.us2.oraclecloud.com,CX_9001",
	"riaorcportal,hcvk.fa.em2.oraclecloud.com,CX_16",
	"northsuburbanfamilyphysicians,fa-etnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_2011",
	"capmetropolicedepartment,fa-eujk-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"banconacionaldecostarica,ehuk.fa.us2.oraclecloud.com,CX_1001",
	"protoconrm,ejdf.fa.us6.oraclecloud.com,CX_1",
	"alternativeparcelslimitednetworkingjobs,fa-esvb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"alternativeparcelslimited,fa-esvb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"uow,efhc.fa.ca2.oraclecloud.com,CX_1",
	"londonmetropolitanuniversity,iaetme.fa.ocs.oraclecloud.com,CX_1002",
	"greenclimatefundjobs,iaayou.fa.ocs.oraclecloud.com,CX_1001",
	"devyanifoodindustrieslimiteddfil,rjcorphcm-iacbiz.fa.ocs.oraclecloud.com,CX_4",
	"eicactivities,elgl.fa.ap1.oraclecloud.com,CX_4001",
	"workersvillagerealestateportal,fa-erbb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1019",
	"geaar,fa-ewnz-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"be,iadygs.fa.ocs.oraclecloud.com,CX_1",
	"federacinpatronal,ibvcjb.fa.ocs.oraclecloud.com,CX_2002",
	"hoffmanequipmentyard,efsp.fa.us6.oraclecloud.com,CX_1",
	"americantowerlatam,hdsn.fa.us6.oraclecloud.com,CX_5001",
	"amavidaatlakespark,eexs.fa.us2.oraclecloud.com,CX_23005",
	"mississippilandbank,eozs.fa.us6.oraclecloud.com,CX_22",
	"africaworldairlines,fa-epqq-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"ugandaairlines,fa-eptm-saasfaprod1.fa.ocs.oraclecloud.com,CX_4001",
	"merexinvestment,esbe.fa.em8.oraclecloud.com,CX_4001",
	"southernraillink,fa-evwo-saasfaprod1.fa.ocs.oraclecloud.com,CX_2003",
	"washingtoncorporations,fa-evwo-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"miraltalentacquisition,enpk.fa.em8.oraclecloud.com,CX_1004",
	"sitocarrieregruppoposteitaliane,fa-emza-saasfaprod1.fa.ocs.oraclecloud.com,CX_3001",
	"phgraf,fa-eutv-saasfaprod1.fa.ocs.oraclecloud.com,CX_5007",
	"amnsindia,emfg.fa.em4.oraclecloud.com,CX_5001",
	"heritagevillageassistedlivingandmemoryca,eexs.fa.us2.oraclecloud.com,CX_8015",
	"clarendaleofmokena,eexs.fa.us2.oraclecloud.com,CX_8017",
	"virginian,eexs.fa.us2.oraclecloud.com,CX_12065",
	"trainingcommunity,eexs.fa.us2.oraclecloud.com,CX_18005",
	"asburyvillage,eexs.fa.us2.oraclecloud.com,CX_12025",
	"uksbs,fa-evzn-saasfaukgovprod1.fa.ocs.oraclecloud.com,CX_5",
	"manhattandainternopportunities,fa-elzs-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"kopcoat,hcwx.fa.us2.oraclecloud.com,CX_6001",
	"svkmsacharyaavpateljrcollege,fa-elxu-saasfaprod1.fa.ocs.oraclecloud.com,CX_1016",
	"coherentnetherlands,hcwp.fa.us2.oraclecloud.com,CX_11006",
	"notified,eclz.fa.us2.oraclecloud.com,CX_3001",
	"centraltexasfarmcredit,eozs.fa.us6.oraclecloud.com,CX_19",
	"colonialfarmcredit,eozs.fa.us6.oraclecloud.com,CX_20",
	"diversityandinclusion,hdcs.fa.ap1.oraclecloud.com,CX_13007",
	"eghj,eghj.fa.em2.oraclecloud.com,CX_8001",
	"espo,eism.fa.em2.oraclecloud.com,CX_2001",
	"ciklumgeneralreferral,ialmme.fa.ocs.oraclecloud.com,CX_2001",
	"fedcapcanada,eckb.fa.us2.oraclecloud.com,CX_1019",
	"fedcapwildcat,eckb.fa.us2.oraclecloud.com,CX_1007",
	"vyllatitle,emvo.fa.us2.oraclecloud.com,CX_3001",
	"hayleysleisure,emdm.fa.ap1.oraclecloud.com,CX_10001",
	"hayleysfabric,emdm.fa.ap1.oraclecloud.com,CX_14001",
	"mabrocteas,emdm.fa.ap1.oraclecloud.com,CX_18001",
	"defenceaerospace,eibd.fa.em2.oraclecloud.com,CX_2015",
	"bcc,enre.fa.em3.oraclecloud.com,CX_3001",
	"poweryouth,ibqhjb.fa.ocs.oraclecloud.com,CX_1005",
	"sfps,ibzbjb.fa.ocs.oraclecloud.com,CX_3",
	"aesindustrial,ekkt.fa.us2.oraclecloud.com,CX_11001",
	"dubaiworldtradecentre,bun.fa.em2.oraclecloud.com,CX_2001",
	"volunteer,fa-evlb-saasfaprod1.fa.ocs.oraclecloud.com,CX_2001",
	"emaarmisr,fa-ewtd-saasfaprod1.fa.ocs.oraclecloud.com,CX_7001",
	"greenmountainhighereducationconsortium,egqw.fa.us2.oraclecloud.com,CX_11",
	"ektz,ektz.fa.us6.oraclecloud.com,CX",
	"carriredistrictscolairefrancophone,emgi.fa.ca3.oraclecloud.com,CX_3003",
	"globalcatalogue,eihb.fa.em3.oraclecloud.com,CX_1001",
	"candidateexperiencemosaico,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_4005",
	"candidateexperiencevaga,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_4013",
	"candidateexperienceadesital,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_4009",
	"candidateexperiencecercol,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_3005",
	"candidateexperienceprofilpas,fa-elhu-saasfaprod1.fa.ocs.oraclecloud.com,CX_5009",
	"atworkplacenl,fa-etna-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"rjretail,rjcorphcm-iacbiz.fa.ocs.oraclecloud.com,CX_2001",
	"sitiodeexperienciadecandidatos,enyy.fa.us2.oraclecloud.com,CX",
	"bupaacbademsigorta,elvh.fa.em2.oraclecloud.com,CX_2001",
	"baberghandmidsuffolkdistrictcouncilsinte,eoce.fa.em3.oraclecloud.com,CX_20004",
	"fhhinternships,fa-emmq-saasfaprod1.fa.ocs.oraclecloud.com,CX_7001",
	"ccas,fa-epeq-saasfaprod1.fa.ocs.oraclecloud.com,CX_2002",
	"hirmasrealestateportal,fa-erbb-saasfaprod1.fa.ocs.oraclecloud.com,CX_19",
	"bridgewellnesshubclubportal,fa-erbb-saasfaprod1.fa.ocs.oraclecloud.com,CX_40",
	"alrahavillagepropertiesportal,fa-erbb-saasfaprod1.fa.ocs.oraclecloud.com,CX_13",
	"okcfire,fa-etyr-saasfaprod1.fa.ocs.oraclecloud.com,CX_1",
	"okcpolice,fa-etyr-saasfaprod1.fa.ocs.oraclecloud.com,CX_4",
	"sitocarrierebper,fa-ewnv-saasfaprod1.fa.ocs.oraclecloud.com,CX_1001",
	"vrainternsgraduates,fa-ewye-saasfaprod1.fa.ocs.oraclecloud.com,CX_7001",
	"vikiai,iabqiz.fa.ocs.oraclecloud.com,CX_1001",
	"earthsown,ibanqy.fa.ocs.oraclecloud.com,CX_1",
	"americantowermexico,hdsn.fa.us6.oraclecloud.com,CX_1005",
	"americantowerspain,hdsn.fa.us6.oraclecloud.com,CX_6005",
	"iarc,estm.fa.em2.oraclecloud.com,CX_10001",
	"talentonevada,fa-enfw-saasfaprod1.fa.ocs.oraclecloud.com,CX_4005",
	"phdtvietnam,eczd.fa.us2.oraclecloud.com,CX_19001",
	"cleanharborsusinternship,epyc.fa.us2.oraclecloud.com,CX_3001",
	"enuf,enuf.fa.us2.oraclecloud.com,CX_8001",
	"tamweenhospitalityportal,fa-erbb-saasfaprod1.fa.ocs.oraclecloud.com,CX_1016",
	"uowcollegeaustralia,ejgl.fa.ap1.oraclecloud.com,CX_4004",
}

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
// "expand=requisitionList" is mandatory and not an optimisation knob. Measured
// against ehac.fa.us6.oraclecloud.com/CX_1: with the expand the response is
// ~343 KB carrying 200 requisitions, and with it dropped entirely the same
// request returns 6.5 KB containing the search facets and NO requisitionList key
// at all — which this adapter would report as a changed layout, correctly, for
// every tenant at once.
//
// What the expand does not need is the ".secondaryLocations" suffix the
// reference implementations carry. That suffix costs ~3% of the payload (353 KB
// against 343 KB on the same page) for a field this adapter does not decode, and
// only 8% of the 6,780 requisitions sampled across 1,501 live tenants had a
// non-empty one. Verified equivalent on four tenants spanning all three host
// shapes (eluq/ehac "eNNN.fa.usN", fa-etjg-saasfaprod1.fa.ocs, jpmc.fa).
func oracleCloudListURL(tenant oracleCloudTenant, offset int) string {
	finder := fmt.Sprintf("findReqs;siteNumber=%s,limit=%d,offset=%d,sortBy=POSTING_DATES_DESC",
		tenant.site, oracleCloudPageSize, offset)

	return fmt.Sprintf("https://%s/hcmRestApi/resources/latest/recruitingCEJobRequisitions?onlyData=true&expand=requisitionList&finder=%s",
		tenant.host, oracleCloudFinderEscape(finder))
}

// oracleCloudPostingURL builds the public candidate-experience URL for one
// requisition.
//
// Verified live, which it previously was not: fetching
// eluq.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_2001/job/203341
// — the template applied to the first requisition the list returned for Kroger —
// answers HTTP 200 with og:title "Night Stocker Clerk", the same title the list
// gave for that id. The site number in the path is the same one the finder is
// addressed by.
func oracleCloudPostingURL(tenant oracleCloudTenant, id string) string {
	return fmt.Sprintf("https://%s/hcmUI/CandidateExperience/en/sites/%s/job/%s", tenant.host, tenant.site, id)
}

// oracleCloudResponse is the subset of the recruitingCEJobRequisitions response
// this adapter reads.
//
// The whole payload hangs off a single-element "items" array, with the total and
// the requisitions as siblings inside it — an artefact of the Fusion REST
// framework rather than a shape anyone would design. Confirmed against live
// responses: the envelope also carries count/hasMore/limit/offset/links at the
// top level and ~40 search-state fields beside "requisitionList" inside the
// item, none of which say anything this adapter needs.
type oracleCloudResponse struct {
	Items []oracleCloudItem `json:"items"`
}

// oracleCloudItem is the single element of the response's "items" array.
type oracleCloudItem struct {
	// TotalJobsCount is how many requisitions the site has in total, which is
	// what makes offset paging terminate on a count rather than on a short page.
	//
	// Live tenants send it as a JSON number; it stays `any` for the same reason
	// as the fields on [oracleCloudRequisition] — one field with an unexpected
	// JSON type fails the decode for the entire page, which loses a whole
	// tenant, and `any` cannot fail a decode.
	//
	// It is not stable to the row: successive requests to the same site minutes
	// apart returned 15119 and 15120. It is a search count, not a ledger, so it
	// is used to decide when to stop asking and never to check what arrived.
	TotalJobsCount any `json:"TotalJobsCount"`

	RequisitionList []oracleCloudRequisition `json:"requisitionList"`
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
// Title and PrimaryLocation are the exception, and are now confirmed strings on
// all 6,780 requisitions sampled across 1,501 live tenants.
//
// The population rates quoted below are from that same sample, taken 2026-07-28.
type oracleCloudRequisition struct {
	// Id is the requisition's identifier within the tenant, and the id the
	// candidate-experience URL is keyed by. Present and a string on 100% of the
	// sample, but not always numeric ("R3326-2" appears), so it is neither
	// parsed nor renumbered.
	ID any `json:"Id"`

	// Title and PrimaryLocation are present on 100% of the sample.
	Title string `json:"Title"`

	PrimaryLocation string `json:"PrimaryLocation"`

	// PostedDate is present on 100% of the sample and is a bare "2026-07-28"
	// calendar date, never a timestamp.
	PostedDate any `json:"PostedDate"`

	// JobSchedule is where this platform actually publishes "Full time" /
	// "Part time": 71 of 6,780 sampled requisitions carry it.
	//
	// This adapter previously read JobType instead, on the strength of the field
	// list in docs/research/ats-platform-survey.md. JobType was populated on
	// ZERO of the 6,780 — every registered tenant's employment type was silently
	// empty. JobType is still decoded because a tenant that does send it costs
	// nothing to support, but JobSchedule is the field that carries the value.
	JobSchedule any `json:"JobSchedule"`

	JobType any `json:"JobType"`

	// WorkplaceTypeCode is Oracle's genuine three-state workplace field, and it
	// is very much in the list response: 2,084 of 6,780 sampled requisitions
	// (30%) carry it. The survey listed it as a detail-endpoint field, which
	// would have made this project pay a request per posting for something it
	// already has.
	//
	// The live spelling is ORA_ON_SITE, not the ORA_ONSITE the survey records.
	// Both normalize, because [internal.NormalizeWorkplaceType] squashes
	// separators before matching, but the documented value was wrong.
	WorkplaceTypeCode any `json:"WorkplaceTypeCode"`

	// WorkplaceType is the human-readable twin of WorkplaceTypeCode, populated
	// on exactly the same rows. It is read as a fallback because it carries
	// spellings the code does not — "On-site with Flexibility", "Hybrid
	// working", the French "Hybride" — all of which normalize on the word they
	// contain, and because a tenant sending one without the other costs nothing
	// to survive.
	WorkplaceType any `json:"WorkplaceType"`

	// Department, JobFunction and JobFamily are three spellings of this
	// project's department, in descending order of how specific they are. All
	// three are rare in the list — 45, 20 and 35 of 6,780 respectively — and all
	// three are decoded opportunistically, exactly as greenhouse.go decodes
	// "first_published" from a list that does not promise it. Tenants that send
	// one get a real department for free; the rest leave it empty.
	Department any `json:"Department"`

	JobFunction any `json:"JobFunction"`

	JobFamily any `json:"JobFamily"`
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

// oracleCloudFirstLabel returns the first of several alternative spellings of
// one concept that a requisition actually carries.
//
// Every field it is called with is sparse — the busiest is populated on 30% of
// requisitions and the rest on well under 1% — so "the first one that is there"
// is the whole rule. Order matters only in that the caller lists the most
// specific field first.
func oracleCloudFirstLabel(values ...any) string {
	for _, value := range values {
		if label := oracleCloudLabel(value); label != "" {
			return label
		}
	}

	return ""
}

// oracleCloudDateLayouts are the timestamp spellings accepted for PostedDate.
//
// Every one of the 6,780 sampled requisitions sent a bare calendar date, so
// "2006-01-02" is the layout that matters and the rest are kept as cheap
// insurance. Only unambiguous ones, for the reason [phenomDateLayouts] spells
// out: a slash-separated date is a different day in the US and in Europe, and
// nothing in this response says which a tenant means. A date a month wrong would
// sit inside [internal.Filter.PostedSince] where nothing downstream could notice
// it.
var oracleCloudDateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
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

// oracleCloudRequisitionIDs lists a page's requisition ids, which is what
// [pageRepeatGuard] fingerprints.
func oracleCloudRequisitionIDs(requisitions []oracleCloudRequisition) []string {
	ids := make([]string, 0, len(requisitions))

	for _, requisition := range requisitions {
		ids = append(ids, anyText(requisition.ID))
	}

	return ids
}

// oracleCloudOffsets plans every page after the first, and reports whether
// [oracleCloudMaxPages] rather than the data is what ended the plan.
//
// step is what the first page actually held, not what was asked for. Several
// boards in this ecosystem answer with fewer rows than the requested limit and
// still expect the caller to keep walking — ADP's public API is documented to do
// exactly that — and an offset stepped by the page size would then skip every
// row past the first page's worth.
//
// The walk ends at whichever comes first: the tenant's own count, the platform's
// [oracleCloudMaxWindow], or the page backstop. A short page is deliberately not
// an ending here — a tenant whose server caps its page size below the requested
// limit would otherwise stop at that cap, silently publishing a fraction of a
// 15,000-posting employer with no error anywhere.
func oracleCloudOffsets(total int, totalOK bool, step int) (offsets []int, capped bool) {
	// Without a count there is nothing to stop on but the window, and an empty
	// page ends the walk at run time.
	end := oracleCloudMaxWindow
	if totalOK && total < end {
		end = total
	}

	for offset := step; offset < end; offset += step {
		// The window is on the request, not on the response: Oracle refuses
		// offset+limit past oracleCloudMaxWindow outright, so a page that would
		// straddle the wall is never asked for.
		if offset+oracleCloudPageSize > oracleCloudMaxWindow {
			break
		}

		if len(offsets) >= oracleCloudMaxPages-1 {
			return offsets, true
		}

		offsets = append(offsets, offset)
	}

	return offsets, false
}

// OracleCloud returns all of the job postings for one Oracle Recruiting Cloud
// careers site, or an error if there was a problem making a request or reading a
// response.
//
// company is a "slug,faHost,siteNumber" triple, see [OracleCloudTenants]; it is
// not a board slug like most platforms here.
//
// The first page carries the tenant's total, so every remaining page offset is
// known immediately; they are fetched with bounded concurrency
// ([oracleCloudPageFetchers]) and their postings are yielded as they arrive
// rather than in page order. This is the [Workday] pattern and it applies for
// the same reason: Oracle gives every tenant its own host, so the per-service
// limiter key is per employer and page requests for one employer are not
// competing with the rest of the platform for the same four slots. It would be
// wrong on Greenhouse or Ashby, where one hostname serves every board.
func OracleCloud(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		tenant, err := parseOracleCloudTenant(company)
		if err != nil {
			yield(nil, err)

			return
		}

		// Cancelling this context is how a consumer that stops early, or a page
		// that fails, tells the in-flight fetchers to wind down at once. The
		// caller's context is kept separately so "the caller cancelled us" can
		// be told apart from "we cancelled ourselves on the way out".
		parentCtx := ctx

		ctx, cancel := context.WithCancel(parentCtx)
		defer cancel()

		// emit hands one page's requisitions to the consumer. It reports whether
		// iteration should continue; a false result means either the consumer
		// asked to stop or the caller's context was cancelled, and in the latter
		// case the error has already been yielded.
		emit := func(requisitions []oracleCloudRequisition) bool {
			for _, requisition := range requisitions {
				if err := parentCtx.Err(); err != nil {
					yield(nil, err)

					return false
				}

				posting := oracleCloudPosting(tenant, requisition)
				if posting == nil {
					continue
				}

				if !yield(posting, nil) {
					return false
				}
			}

			return true
		}

		first, err := oracleCloudFetchPage(ctx, httpClient, tenant, 0)
		if err != nil {
			yield(nil, err)

			return
		}

		total, totalOK := oracleCloudTotal(first.TotalJobsCount)
		step := len(first.RequisitionList)

		if step == 0 {
			// A first page with neither requisitions nor a readable total is a
			// shape this adapter does not recognise, not an empty board.
			if !totalOK {
				yield(nil, fmt.Errorf("unexpected response from Oracle Cloud for company %q (site %s on %s): the response carried neither a requisition list nor a job count, so its layout may have changed",
					tenant.slug, tenant.site, tenant.host))
			}

			return
		}

		// The guard is fed the first page here so that a tenant which answers
		// every offset with it is recognised on the second page rather than on
		// the third.
		var pages pageRepeatGuard

		pages.repeated(oracleCloudRequisitionIDs(first.RequisitionList))

		if !emit(first.RequisitionList) {
			return
		}

		// capped cannot be true with no offsets — reaching the backstop means
		// oracleCloudMaxPages-1 of them were planned — so a tenant that fits in
		// one page is finished here.
		offsets, capped := oracleCloudOffsets(total, totalOK, step)
		if len(offsets) == 0 {
			return
		}

		type pageResult struct {
			item *oracleCloudItem
			err  error
		}

		var (
			results   = make(chan pageResult)
			sem       = make(chan struct{}, oracleCloudPageFetchers)
			exhausted = make(chan struct{})
			exhaust   sync.Once
			wg        sync.WaitGroup
		)

		// stopScheduling is called when a page comes back with no requisitions
		// at all, or repeats one already seen. The first means the tenant's
		// reported total overshot what it will serve, the second means it is
		// ignoring the offset entirely; either way the offsets past that point
		// would only fetch pages worth nothing.
		//
		// A short but non-empty page is deliberately not treated this way. It is
		// indistinguishable from a tenant hiccuping mid-crawl, and acting on it
		// would silently truncate an employer, which is precisely the class of
		// bug this code exists to prevent.
		//
		// It also clears the page-ceiling report. Planning more pages than
		// oracleCloudMaxPages allows and then discovering the site had fewer is
		// not a site that outran the backstop, and saying so would fail a source
		// that finished cleanly.
		var ranOut bool

		stopScheduling := func() {
			ranOut = true

			exhaust.Do(func() { close(exhausted) })
		}

		go func() {
			// results must not be closed until every sender has finished, or a
			// straggling send would panic.
			defer func() {
				wg.Wait()
				close(results)
			}()

			for _, offset := range offsets {
				// Checked before the selects below because a select with two
				// ready cases picks at random, which would let a new request
				// start after cancellation.
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

				// A slot may have been ready at the same moment as a stop
				// signal, and select picks at random between ready cases.
				select {
				case <-exhausted:
					<-sem

					return
				default:
				}

				wg.Add(1)

				go func(offset int) {
					defer wg.Done()
					// The slot is held until the result has been handed over, so
					// prefetching runs at most oracleCloudPageFetchers pages
					// ahead of the consumer instead of buffering a whole tenant
					// in memory.
					defer func() { <-sem }()

					item, err := oracleCloudFetchPage(ctx, httpClient, tenant, offset)

					select {
					case results <- pageResult{item: item, err: err}:
					case <-ctx.Done():
					}
				}(offset)
			}
		}()

		// stop unwinds the fan-out before returning: cancel, then drain until
		// results is closed, which only happens once every fetcher has exited.
		// Returning without draining would leave goroutines running past the end
		// of the iterator.
		stop := func() {
			cancel()

			for range results {
			}
		}

		for result := range results {
			if result.err != nil {
				stop()
				yield(nil, result.err)

				return
			}

			if len(result.item.RequisitionList) == 0 {
				stopScheduling()

				continue
			}

			// Checked before anything is yielded, so a tenant that ignores
			// "offset" costs the pages already in flight rather than a stream of
			// duplicates that [internal.Dedupe] would hide.
			if pages.repeated(oracleCloudRequisitionIDs(result.item.RequisitionList)) {
				stopScheduling()

				continue
			}

			if !emit(result.item.RequisitionList) {
				stop()

				return
			}
		}

		// A tenant cut short by the caller's cancellation returned partial
		// results. Say so, rather than let a truncated employer look complete.
		if err := parentCtx.Err(); err != nil {
			yield(nil, err)

			return
		}

		if capped && !ranOut {
			yield(nil, oracleCloudPageCeilingError(tenant))
		}
	}
}

// oracleCloudPageCeilingError reports a tenant cut short by
// [oracleCloudMaxPages].
//
// Deliberately distinct from being cut short by [oracleCloudMaxWindow], which is
// the platform behaving as measured and is not an error: reaching the page
// backstop means a tenant served so few rows per page that 500 requests did not
// exhaust a 10,000-row window, which is a shape nobody here has seen.
func oracleCloudPageCeilingError(tenant oracleCloudTenant) error {
	return fmt.Errorf("refusing to keep paginating Oracle Cloud for company %q (site %s on %s): the site was still serving pages after %d requests inside its %d-row window, so it may be serving far fewer rows per page than the %d asked for",
		tenant.slug, tenant.site, tenant.host, oracleCloudMaxPages, oracleCloudMaxWindow, oracleCloudPageSize)
}

// oracleCloudFetchPage fetches one page of a tenant's requisition list.
//
// The envelope is always one item, even for a site with no jobs. An empty items
// array is Oracle answering something other than this API — a maintenance page,
// a site number that does not exist — and reporting it as an employer with no
// openings is precisely the silent failure this project fears most.
func oracleCloudFetchPage(ctx context.Context, httpClient *http.Client, tenant oracleCloudTenant, offset int) (*oracleCloudItem, error) {
	// fetchJSON closes the response body before it returns, so a fifty-page
	// tenant cannot accumulate open bodies, and its errors already name the
	// platform and the tenant.
	doc, err := fetchJSON[oracleCloudResponse](ctx, httpClient, "Oracle Cloud", tenant.slug, jsonRequest{
		URL: oracleCloudListURL(tenant, offset),
	})
	if err != nil {
		return nil, err
	}

	if len(doc.Items) == 0 {
		return nil, fmt.Errorf("unexpected response from Oracle Cloud for company %q (site %s on %s): no items in the requisition list response, so the site number may be wrong or the API may have changed",
			tenant.slug, tenant.site, tenant.host)
	}

	return &doc.Items[0], nil
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

		Department: oracleCloudFirstLabel(requisition.Department, requisition.JobFunction, requisition.JobFamily),

		// The list publishes one identifier and it is the ATS's, the key the
		// candidate-experience route is addressed by. The employer's own
		// requisition number is a separate field on this platform and was not
		// present on any of the 6,780 requisitions sampled from the list
		// endpoint, so RequisitionID is left empty rather than filled with
		// something that is not one.
		ExternalID: id,

		Source: internal.PostingSource{
			Platform: oracleCloudPlatform,
			Key:      tenant.key,
		},
	}

	// An unrecognised spelling leaves the field empty rather than guessing: a
	// wrong employment type cannot be told apart from a right one by a filter,
	// while an absent one is visibly absent.
	if employment, ok := internal.NormalizeEmploymentType(oracleCloudFirstLabel(requisition.JobSchedule, requisition.JobType)); ok {
		posting.EmploymentType = employment
	}

	// ORA_ON_SITE / ORA_HYBRID / ORA_REMOTE all normalize on the word they
	// contain once separators are squashed, so Oracle's prefix needs no special
	// case here.
	if workplace, ok := internal.NormalizeWorkplaceType(oracleCloudFirstLabel(requisition.WorkplaceTypeCode, requisition.WorkplaceType)); ok {
		posting.WorkplaceType = workplace
	}

	if posted, ok := oracleCloudPostedAt(requisition.PostedDate); ok {
		posting.PostedAt = posted
	}

	return posting
}
