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
	// each of the 1,626 hosts would get its own four-slot limiter and this
	// platform alone could put 6,504 concurrent requests on one backend. That is
	// the shape that rate-limited 56 Workable boards into looking dead.
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
//
// # This list is measured, not staged
//
// It was built in two passes, both against the candidate file at
// testdata/candidates/icims_hosts.txt, whose header carries the full numbers.
//
// 2026-07-28: 321 hosts probed at
// https://{host}/jobs/search?pr=0&in_iframe=1, 70 of them walked to the last
// page (778 requests, 19,922 distinct posting URLs), 63 registered — the 70
// minus the seven the next section explains. The walk priced the platform:
//
//	63 boards, 649 HTTP requests, 15,932 distinct posting URLs
//	= 24.5 postings per request
//
// That number is the reason this platform is registered at all.
// docs/research/ats-platform-survey.md put iCIMS-classic among the lanes that
// "will blow the time budget" and estimated any JSON-LD detail-walk route
// "below 1 posting per request". The estimate is right about the route it
// describes -- sitemap.xml plus schema.org JSON-LD per job page really is one
// request per posting -- and wrong about this one. 24.5 puts the classic search
// route above Teamtailor (~14), Personio (~10), ADP (~8) and Paylocity (~2) on
// the survey's own ranking, and just above Pinpoint (~21).
//
// 2026-07-30: the remaining 1,990 staged hosts were probed live against the
// candidate file's own promotion rule (page 0 answers 200 AND carries at least
// one job card holding a same-host /jobs/{id}/ anchor — content, never status,
// because iCIMS serves HTML). 1,872 passed; 1,563 of them are registered below
// alongside the walked 63. The 309 held back are each annotated in the
// candidate file: 237 whose derived company name already exists in this
// registry (the double-count risk the next section measured, generalized —
// they stay staged until a URL/title comparison issues a verdict), 64 that
// share page-0 posting ids with a larger board of the same iCIMS account
// (language mirrors and portal copies; ids were compared across all 480
// same-name clustered hosts, and sharing even one id is what a mirror looks
// like from one page), 3 internal/employee-only boards, and 5 whose host
// derives to a generic name ("careers") that --company could never find.
// Their page-0 floor is 32,068 postings across the 1,563; 916 of the 1,872
// published a <link rel="next">, so full boards are larger.
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
// here, and neither is any other host whose derived company name is already in
// the registry — see the candidate file's per-line annotations for all 237.
var ICIMSHosts = []string{
	"abudhabi-nyu.icims.com",
	"academiccareers-udst.icims.com",
	"accesssolutions-skyclimber.icims.com",
	"acip-generalrv.icims.com",
	"admcareers-redcoats.icims.com",
	"administrators-ocps.icims.com",
	"admiralsecuritycareers-redcoats.icims.com",
	"advantagecarecareers-ahrc.icims.com",
	"afscareers-epikafleet.icims.com",
	"aircomfort-solutionstulsa.icims.com",
	"alabamaexternal-mobisalabamallc.icims.com",
	"alexleecareers-alexlee.icims.com",
	"alleganynursingandrehab-careers-fundltc.icims.com",
	"alliedhealth-beaumonthospital.icims.com",
	"amazinglashstudio-wellbizbrands.icims.com",
	"americas-cookmedical.icims.com",
	"amplify-hearing.icims.com",
	"andrewjackson-puzzlehr.icims.com",
	"apac-blackhawknetwork.icims.com",
	"apac-cookmedical.icims.com",
	"apaccareers-trinseo.icims.com",
	"app-baycaremedicalgroup.icims.com",
	"app-career-curanahealth.icims.com",
	"application-oneblood.icims.com",
	"apply-fastenterprises.icims.com",
	"apply-servicon.icims.com",
	"arbor-metals.icims.com",
	"arc-2wglobal.icims.com",
	"arc-onesolutions.icims.com",
	"artemisarc-aptiveresources.icims.com",
	"asl-departmentdirectorwagedisplay.icims.com",
	"asl-frontlinewagedisplay.icims.com",
	"atcareers-jaggaer.icims.com",
	"atlanticdetroitdieselallison-kirbycorp.icims.com",
	"attorney-ebglaw.icims.com",
	"aus-truesociety.icims.com",
	"australia-geosyntec.icims.com",
	"australia-kleinfelder.icims.com",
	"autismspectrum-learnbehavioral.icims.com",
	"auxiliary-fisd.icims.com",
	"baca-learnbehavioral.icims.com",
	"behavioralconcepts-learnbehavioral.icims.com",
	"beitzelcareers-beitzelpillar.icims.com",
	"belltowerskillednursing-careers-fundltc.icims.com",
	"bennettsvillehrc-careers-fundltc.icims.com",
	"berlinnursingandrehab-careers-fundltc.icims.com",
	"bi-incorporated-geogroup.icims.com",
	"bicon-bi-conservices.icims.com",
	"birdsongcareers-demant.icims.com",
	"bonobos-guideshop.icims.com",
	"bridgecrestsuites-careers-fundltc.icims.com",
	"brookvillecareers-ahrc.icims.com",
	"bughouse-rollins.icims.com",
	"cabincrew-riyadhair.icims.com",
	"caf-erac.icims.com",
	"campus-hcsgcorp.icims.com",
	"canada-careers-carollo.icims.com",
	"canada-careers-kimley-horn.icims.com",
	"canada-english-avisonyoung.icims.com",
	"canada-envoyair.icims.com",
	"canadacareers-fujifilm.icims.com",
	"canadacareers-hines.icims.com",
	"career-fairs-quiddity.icims.com",
	"career-goldbeltshareholder.icims.com",
	"career-joevsmartshop.icims.com",
	"career-oceansideserviceinc.icims.com",
	"career-opportunities-thezenith.icims.com",
	"career-petersburg.icims.com",
	"career-portal-wolfandco.icims.com",
	"career-realalloy.icims.com",
	"career-schwab.icims.com",
	"career24-oremorautomotive.icims.com",
	"careerportaln-vistacommunityclinic.icims.com",
	"careerportalp-vistacommunityclinic.icims.com",
	"careers-48forty.icims.com",
	"careers-4s.icims.com",
	"careers-aaalife.icims.com",
	"careers-aacr.icims.com",
	"careers-aadermatology.icims.com",
	"careers-abacustech.icims.com",
	"careers-acadiahealthcare.icims.com",
	"careers-accoy.icims.com",
	"careers-acehome.icims.com",
	"careers-aceroschools.icims.com",
	"careers-actalentservices.icims.com",
	"careers-actslife.icims.com",
	"careers-adl.icims.com",
	"careers-adlittle.icims.com",
	"careers-adspipe.icims.com",
	"careers-advancemgt.icims.com",
	"careers-advocatesinc.icims.com",
	"careers-aei.icims.com",
	"careers-aeieng.icims.com",
	"careers-aeropostale.icims.com",
	"careers-aerotek.icims.com",
	"careers-affiniuscapital.icims.com",
	"careers-ahmchealth.icims.com",
	"careers-aidshealth.icims.com",
	"careers-aircomfort.icims.com",
	"careers-airmethods.icims.com",
	"careers-aitworldwide.icims.com",
	"careers-albania-titanmaterials.icims.com",
	"careers-alcami.icims.com",
	"careers-allegisgroup.icims.com",
	"careers-allison.icims.com",
	"careers-allnativegroup.icims.com",
	"careers-allserviceprofessionals.icims.com",
	"careers-allwayscaring.icims.com",
	"careers-altadis.icims.com",
	"careers-altapointe.icims.com",
	"careers-altorfer.icims.com",
	"careers-alumus.icims.com",
	"careers-ambest.icims.com",
	"careers-americafirst.icims.com",
	"careers-americanaddictioncenters.icims.com",
	"careers-americanfoodsgroup.icims.com",
	"careers-americanhouse.icims.com",
	"careers-americanseniorbenefits.icims.com",
	"careers-americansystems.icims.com",
	"careers-ameritfleet.icims.com",
	"careers-ammechanical.icims.com",
	"careers-amtrustgroup.icims.com",
	"careers-anothersource.icims.com",
	"careers-anumber1air.icims.com",
	"careers-apac-berkley.icims.com",
	"careers-apexservicepartners.icims.com",
	"careers-aphl.icims.com",
	"careers-apogeeusa.icims.com",
	"careers-apolloretail.icims.com",
	"careers-appleairli.icims.com",
	"careers-application-racker.icims.com",
	"careers-appliedsystems.icims.com",
	"careers-applusna.icims.com",
	"careers-arcticairav.icims.com",
	"careers-arh.icims.com",
	"careers-arizonacollege.icims.com",
	"careers-arrowtransportation.icims.com",
	"careers-ars.icims.com",
	"careers-artemisgoldinc.icims.com",
	"careers-aspireindiana.icims.com",
	"careers-assemblyeu.icims.com",
	"careers-assemblyna.icims.com",
	"careers-assertiotx.icims.com",
	"careers-astralatauburn.icims.com",
	"careers-astralatfranklin.icims.com",
	"careers-atcc.icims.com",
	"careers-atchleyair.icims.com",
	"careers-athletico.icims.com",
	"careers-atipt.icims.com",
	"careers-atl.icims.com",
	"careers-atlas-aerospace.icims.com",
	"careers-atlasworldgroupinc.icims.com",
	"careers-attainfinance.icims.com",
	"careers-audacy.icims.com",
	"careers-aurora.icims.com",
	"careers-avian.icims.com",
	"careers-avionyx.icims.com",
	"careers-avon.icims.com",
	"careers-avondale.icims.com",
	"careers-axway.icims.com",
	"careers-bags.icims.com",
	"careers-bakerbrothersplumbing.icims.com",
	"careers-bancorpbank.icims.com",
	"careers-bancroft.icims.com",
	"careers-bandq.icims.com",
	"careers-bank-cffc.icims.com",
	"careers-banknewport.icims.com",
	"careers-bannerlife.icims.com",
	"careers-banyanglobal.icims.com",
	"careers-baptisthealthal.icims.com",
	"careers-baptistneighborhoodhospital.icims.com",
	"careers-barkerservices.icims.com",
	"careers-barrios.icims.com",
	"careers-barton-supply.icims.com",
	"careers-bartwest.icims.com",
	"careers-bassettservices.icims.com",
	"careers-bayfronthealth.icims.com",
	"careers-bcferries.icims.com",
	"careers-bcore.icims.com",
	"careers-bdobelgium.icims.com",
	"careers-bdssolutions.icims.com",
	"careers-belardiwong.icims.com",
	"careers-benaroyaresearch.icims.com",
	"careers-benefitcosmeticsuk.icims.com",
	"careers-bergelectric.icims.com",
	"careers-berkeys.icims.com",
	"careers-berkley.icims.com",
	"careers-berkleycaregroup.icims.com",
	"careers-berrydunn.icims.com",
	"careers-betterbuzzcoffee.icims.com",
	"careers-betterhealthgroup.icims.com",
	"careers-bevifr-vincotte.icims.com",
	"careers-bevinl-vincotte.icims.com",
	"careers-beyondsoft.icims.com",
	"careers-bgca.icims.com",
	"careers-bhshealth.icims.com",
	"careers-biorad.icims.com",
	"careers-blackhawk.icims.com",
	"careers-blackhawknetwork.icims.com",
	"careers-blanchard.icims.com",
	"careers-blarneycastleoil.icims.com",
	"careers-bluegracegroup.icims.com",
	"careers-bluehawaiian.icims.com",
	"careers-bluehawk.icims.com",
	"careers-blueheron.icims.com",
	"careers-bluesprigpediatrics.icims.com",
	"careers-blythedale.icims.com",
	"careers-bncollege.icims.com",
	"careers-bncsystems.icims.com",
	"careers-bocusa.icims.com",
	"careers-boothes.icims.com",
	"careers-bosselman.icims.com",
	"careers-bostonpizza.icims.com",
	"careers-bradfordexchange.icims.com",
	"careers-brandesassociates.icims.com",
	"careers-brazil-magnera.icims.com",
	"careers-bridgenext.icims.com",
	"careers-brightonhospice.icims.com",
	"careers-brightviewhealth.icims.com",
	"careers-brinkmannconstructors.icims.com",
	"careers-bronxcare.icims.com",
	"careers-brookdalecc.icims.com",
	"careers-brookhavenhospice.icims.com",
	"careers-brookings.icims.com",
	"careers-brooksbrothers.icims.com",
	"careers-bruniandcampisi.icims.com",
	"careers-brutonknowles.icims.com",
	"careers-btd.icims.com",
	"careers-buckeye.icims.com",
	"careers-budhvac.icims.com",
	"careers-buildingandearth.icims.com",
	"careers-buspatrol.icims.com",
	"careers-buzziunicemusa.icims.com",
	"careers-bwsc.icims.com",
	"careers-c1.icims.com",
	"careers-cacu.icims.com",
	"careers-cadence-education.icims.com",
	"careers-cadent.icims.com",
	"careers-cajunusa.icims.com",
	"careers-calamp.icims.com",
	"careers-callbuck.icims.com",
	"careers-callhero.icims.com",
	"careers-calltheb.icims.com",
	"careers-calspan.icims.com",
	"careers-cambrianhomecare.icims.com",
	"careers-campusapts.icims.com",
	"careers-canada-secure.icims.com",
	"careers-canfieldph.icims.com",
	"careers-cannacabana.icims.com",
	"careers-capbluecross.icims.com",
	"careers-capreit.icims.com",
	"careers-carehospice.icims.com",
	"careers-carenethealthcare.icims.com",
	"careers-carepartners.icims.com",
	"careers-carepointhealth.icims.com",
	"careers-carle.icims.com",
	"careers-carollo.icims.com",
	"careers-cascade-management.icims.com",
	"careers-catalystbrand.icims.com",
	"careers-cayuseholdings.icims.com",
	"careers-cbc-nybloodcenter.icims.com",
	"careers-cbps.icims.com",
	"careers-ccr-cz.icims.com",
	"careers-ccr-fr.icims.com",
	"careers-ccr-ge.icims.com",
	"careers-ccr.icims.com",
	"careers-ccs-medical.icims.com",
	"careers-ccwestmi.icims.com",
	"careers-cecinc.icims.com",
	"careers-cellularsales.icims.com",
	"careers-centersusa.icims.com",
	"careers-centrastate.icims.com",
	"careers-centricbrands.icims.com",
	"careers-centricbrandsasia.icims.com",
	"careers-cfins.icims.com",
	"careers-cfr.icims.com",
	"careers-cfsny.icims.com",
	"careers-chai.icims.com",
	"careers-championac.icims.com",
	"careers-championcomfortexperts.icims.com",
	"careers-changegrowlive.icims.com",
	"careers-chapmanbros.icims.com",
	"careers-chasebrass.icims.com",
	"careers-chc.icims.com",
	"careers-chcbrazil.icims.com",
	"careers-chordsdp.icims.com",
	"careers-christiancare.icims.com",
	"careers-christianhvac.icims.com",
	"careers-chsi.icims.com",
	"careers-chumashcareers.icims.com",
	"careers-citizensenergygroup.icims.com",
	"careers-citrin.icims.com",
	"careers-citynational.icims.com",
	"careers-civilianmedicaljobs.icims.com",
	"careers-cjsheatingandair.icims.com",
	"careers-clearygottlieb.icims.com",
	"careers-clsliving.icims.com",
	"careers-cmdelectric.icims.com",
	"careers-cnhind.icims.com",
	"careers-codev.icims.com",
	"careers-colliersprojects.icims.com",
	"careers-collinscomfort.icims.com",
	"careers-commitent.icims.com",
	"careers-commtrans.icims.com",
	"careers-communityproviderplus.icims.com",
	"careers-compsych.icims.com",
	"careers-conciergenursing.icims.com",
	"careers-concoracredit.icims.com",
	"careers-concord.icims.com",
	"careers-connectionshs.icims.com",
	"careers-connectiverx.icims.com",
	"careers-consilio-lod.icims.com",
	"careers-consilio.icims.com",
	"careers-consoreng.icims.com",
	"careers-constructconnect.icims.com",
	"careers-containerstore.icims.com",
	"careers-continuumhospice.icims.com",
	"careers-continuumservices.icims.com",
	"careers-coolray.icims.com",
	"careers-cooltoday.icims.com",
	"careers-cooperhealth.icims.com",
	"careers-cooperparry.icims.com",
	"careers-coraltreehospitality.icims.com",
	"careers-cordobacorp.icims.com",
	"careers-cotiviti.icims.com",
	"careers-cougarmechanical.icims.com",
	"careers-councilofindustry.icims.com",
	"careers-country-fair.icims.com",
	"careers-covenanthealth.icims.com",
	"careers-cpicardgroup.icims.com",
	"careers-cravath.icims.com",
	"careers-crescenthospice.icims.com",
	"careers-cslships.icims.com",
	"careers-ctifoods.icims.com",
	"careers-culmen.icims.com",
	"careers-cummingselec.icims.com",
	"careers-cuyahogabdd.icims.com",
	"careers-cvrd.icims.com",
	"careers-cvtc.icims.com",
	"careers-cwsglobal.icims.com",
	"careers-daifuku-america.icims.com",
	"careers-daktronics.icims.com",
	"careers-damar.icims.com",
	"careers-danley911.icims.com",
	"careers-dartmouth-hitchcock.icims.com",
	"careers-davisonheating.icims.com",
	"careers-daytonfreight.icims.com",
	"careers-dcgoodwill.icims.com",
	"careers-de-axa.icims.com",
	"careers-de-kiwa.icims.com",
	"careers-decisionpointcorp.icims.com",
	"careers-deltaworld.icims.com",
	"careers-depaul.icims.com",
	"careers-designworkshop.icims.com",
	"careers-devereux.icims.com",
	"careers-dewberry.icims.com",
	"careers-dggpdn.icims.com",
	"careers-dialysisclinic.icims.com",
	"careers-didiglobal.icims.com",
	"careers-dierksenhospice.icims.com",
	"careers-dminc.icims.com",
	"careers-dohertyinc.icims.com",
	"careers-dohrn.icims.com",
	"careers-dole.icims.com",
	"careers-dreeshomes.icims.com",
	"careers-drfirst.icims.com",
	"careers-dudek.icims.com",
	"careers-duluthtrading.icims.com",
	"careers-dunbia.icims.com",
	"careers-dynamichomes.icims.com",
	"careers-e2.icims.com",
	"careers-eaglebankcorp.icims.com",
	"careers-eaglefoods.icims.com",
	"careers-eaglepicher.icims.com",
	"careers-earlycareers.icims.com",
	"careers-eastpennmanufacturing.icims.com",
	"careers-eastwestbank.icims.com",
	"careers-edf-re.icims.com",
	"careers-edgewaterit.icims.com",
	"careers-edmorse.icims.com",
	"careers-edmundoptics.icims.com",
	"careers-ehhi.icims.com",
	"careers-eimagine.icims.com",
	"careers-electronic-therapy.icims.com",
	"careers-embarkbh.icims.com",
	"careers-emcorgroup.icims.com",
	"careers-emea-sazerac.icims.com",
	"careers-emerus.icims.com",
	"careers-empiricalfoods.icims.com",
	"careers-emscrm.icims.com",
	"careers-en-biorad.icims.com",
	"careers-en-gb-teksystems.icims.com",
	"careers-en-kiwa.icims.com",
	"careers-en-nortal.icims.com",
	"careers-encorefireprotection.icims.com",
	"careers-encyclis.icims.com",
	"careers-endeavorair.icims.com",
	"careers-endeavorschoolsllc.icims.com",
	"careers-enfra.icims.com",
	"careers-englert.icims.com",
	"careers-englewood.icims.com",
	"careers-epeconsulting.icims.com",
	"careers-epsii.icims.com",
	"careers-ermcoeci.icims.com",
	"careers-ers.icims.com",
	"careers-es-kiwa.icims.com",
	"careers-evms.icims.com",
	"careers-exponent.icims.com",
	"careers-expressivebeginningschildcare.icims.com",
	"careers-eyesouthpartners.icims.com",
	"careers-fantesphvac.icims.com",
	"careers-fastenterprises.icims.com",
	"careers-fdcorp.icims.com",
	"careers-fdcorpcan.icims.com",
	"careers-fdmfieldservices.icims.com",
	"careers-federatedinsurance.icims.com",
	"careers-fednot.icims.com",
	"careers-feeltheadvantage.icims.com",
	"careers-ferrellgas.icims.com",
	"careers-festfoods.icims.com",
	"careers-fhcrc.icims.com",
	"careers-fhfurr.icims.com",
	"careers-fhp.icims.com",
	"careers-fi-kiwa.icims.com",
	"careers-filtrationgroupcorp.icims.com",
	"careers-finance-cffc.icims.com",
	"careers-firebirdsrestaurants.icims.com",
	"careers-firstambank.icims.com",
	"careers-flanigans.icims.com",
	"careers-flixmedia.icims.com",
	"careers-floridamedclinic.icims.com",
	"careers-foley.icims.com",
	"careers-fontainebleau.icims.com",
	"careers-fortiss.icims.com",
	"careers-forumcu.icims.com",
	"careers-four-paws.icims.com",
	"careers-fourcorners.icims.com",
	"careers-fr-axatest.icims.com",
	"careers-fr-kiwa.icims.com",
	"careers-fr-soitec.icims.com",
	"careers-framatome.icims.com",
	"careers-framatomecanada.icims.com",
	"careers-france-magnera.icims.com",
	"careers-franciscanministries.icims.com",
	"careers-frankgaycommerical.icims.com",
	"careers-fresenius-kabi.icims.com",
	"careers-freshairinc.icims.com",
	"careers-friendshipschools.icims.com",
	"careers-fsse-srmc.icims.com",
	"careers-ftidefense.icims.com",
	"careers-fult.icims.com",
	"careers-futurecarehealth.icims.com",
	"careers-galapagos.icims.com",
	"careers-gannettfleming.icims.com",
	"careers-gatesair.icims.com",
	"careers-gaynors.icims.com",
	"careers-gbrx.icims.com",
	"careers-gcg.icims.com",
	"careers-gd-ots.icims.com",
	"careers-gdeb.icims.com",
	"careers-gdifamilyofbrands.icims.com",
	"careers-genejohnsonplumbing.icims.com",
	"careers-generaldynamics.icims.com",
	"careers-gentivahs.icims.com",
	"careers-genusplc.icims.com",
	"careers-geosyntec.icims.com",
	"careers-geoyeti.icims.com",
	"careers-germany-magnera.icims.com",
	"careers-gildan.icims.com",
	"careers-girlscouts.icims.com",
	"careers-globalcu.icims.com",
	"careers-goarco.icims.com",
	"careers-gocs.icims.com",
	"careers-goldbeltghs.icims.com",
	"careers-goldenkeygroup.icims.com",
	"careers-goodshepherdhospice.icims.com",
	"careers-goprimegroup.icims.com",
	"careers-gordontruckcenters.icims.com",
	"careers-gotyto.icims.com",
	"careers-gr8global.icims.com",
	"careers-grahampackaging.icims.com",
	"careers-grandviewhealth.icims.com",
	"careers-granicus.icims.com",
	"careers-gray.icims.com",
	"careers-greatdane.icims.com",
	"careers-greece-titanmaterials.icims.com",
	"careers-greenbrickpartners.icims.com",
	"careers-greensky.icims.com",
	"careers-greenstreet.icims.com",
	"careers-greulichs.icims.com",
	"careers-greyhound.icims.com",
	"careers-grizzlies.icims.com",
	"careers-group1auto.icims.com",
	"careers-groupsrecovertogether.icims.com",
	"careers-growfinancial.icims.com",
	"careers-grsm.icims.com",
	"careers-gtsx.icims.com",
	"careers-guelph.icims.com",
	"careers-haart.icims.com",
	"careers-hakimgroup.icims.com",
	"careers-hamiltoncompany.icims.com",
	"careers-hanger.icims.com",
	"careers-harborfoods.icims.com",
	"careers-harmonycares.icims.com",
	"careers-harpercollins.icims.com",
	"careers-harrison-ymca.icims.com",
	"careers-hawaiigas.icims.com",
	"careers-haztekinc.icims.com",
	"careers-hcc-hqn.icims.com",
	"careers-hcisystems.icims.com",
	"careers-hcmhcares.icims.com",
	"careers-hcsgcorp.icims.com",
	"careers-healthedge.icims.com",
	"careers-healthequity.icims.com",
	"careers-healthmanagement.icims.com",
	"careers-healthpro-rehab.icims.com",
	"careers-heart.icims.com",
	"careers-hearthhospice.icims.com",
	"careers-heartlandvetpartners.icims.com",
	"careers-heliohealth.icims.com",
	"careers-hendrixair.icims.com",
	"careers-hepburnandsons.icims.com",
	"careers-here.icims.com",
	"careers-heritagechristianservices.icims.com",
	"careers-heritagehomeservice.icims.com",
	"careers-hexagonpositioning.icims.com",
	"careers-hexpol.icims.com",
	"careers-hgistores.icims.com",
	"careers-hhmlp.icims.com",
	"careers-hhsys.icims.com",
	"careers-highpriority.icims.com",
	"careers-hines.icims.com",
	"careers-hkgroup.icims.com",
	"careers-ho-chunk.icims.com",
	"careers-hockersplumbing.icims.com",
	"careers-hoganandsons.icims.com",
	"careers-holisticarehospice.icims.com",
	"careers-homecareassistance.icims.com",
	"careers-homecomfortusa.icims.com",
	"careers-homesteadhc.icims.com",
	"careers-horizon.icims.com",
	"careers-horton.icims.com",
	"careers-hospiceofchattanooga.icims.com",
	"careers-hospiceofthewest.icims.com",
	"careers-houstonretina.icims.com",
	"careers-hsedne.icims.com",
	"careers-humanscale.icims.com",
	"careers-hunterpr.icims.com",
	"careers-hustlerhollywood.icims.com",
	"careers-hvivo.icims.com",
	"careers-hwkaufman.icims.com",
	"careers-hyland.icims.com",
	"careers-i2xisys.icims.com",
	"careers-i3-corps.icims.com",
	"careers-ibr.icims.com",
	"careers-ideagenen.icims.com",
	"careers-ideagenuk.icims.com",
	"careers-ideagenus.icims.com",
	"careers-idirect.icims.com",
	"careers-idirectgov.icims.com",
	"careers-idlo.icims.com",
	"careers-ieedu.icims.com",
	"careers-imgva.icims.com",
	"careers-immanuel.icims.com",
	"careers-imsa.icims.com",
	"careers-imsheatingandair.icims.com",
	"careers-india-cecoenviro.icims.com",
	"careers-insightcapitalsolutions.icims.com",
	"careers-international-realpagepms.icims.com",
	"careers-internova.icims.com",
	"careers-interstatewaste.icims.com",
	"careers-intrastaff.icims.com",
	"careers-iqt.icims.com",
	"careers-iquw.icims.com",
	"careers-iridium.icims.com",
	"careers-isaca.icims.com",
	"careers-ishpi.icims.com",
	"careers-it-merlinentertainments.icims.com",
	"careers-ita-intl.icims.com",
	"careers-italy-magnera.icims.com",
	"careers-itcmgt.icims.com",
	"careers-ja-axa.icims.com",
	"careers-jarboes.icims.com",
	"careers-jetpolymer.icims.com",
	"careers-jobyaviation.icims.com",
	"careers-jocogov.icims.com",
	"careers-johannafoods.icims.com",
	"careers-jointcommission.icims.com",
	"careers-jonwayne.icims.com",
	"careers-joybakinggroup.icims.com",
	"careers-jpcullen.icims.com",
	"careers-jsco.icims.com",
	"careers-justmortgages.icims.com",
	"careers-jwaffinityit.icims.com",
	"careers-kahlerhospitalitygroup.icims.com",
	"careers-karyopharm.icims.com",
	"careers-kbcomplete.icims.com",
	"careers-kci.icims.com",
	"careers-kemin.icims.com",
	"careers-ketteringhealth.icims.com",
	"careers-kettler.icims.com",
	"careers-kfs.icims.com",
	"careers-kiakahi.icims.com",
	"careers-kidscarehh.icims.com",
	"careers-kimley-horn.icims.com",
	"careers-kinaxis.icims.com",
	"careers-kingsswimacademy.icims.com",
	"careers-kinnerton.icims.com",
	"careers-kiscoseniorliving.icims.com",
	"careers-kleinfelder.icims.com",
	"careers-kloveair1.icims.com",
	"careers-kmco.icims.com",
	"careers-knaufinsulation.icims.com",
	"careers-knowledgeservices.icims.com",
	"careers-ko-merlinentertainments.icims.com",
	"careers-kurita.icims.com",
	"careers-kyfb.icims.com",
	"careers-kyowa-kirin.icims.com",
	"careers-l2tllc.icims.com",
	"careers-lambstire.icims.com",
	"careers-lbusa.icims.com",
	"careers-leafguard.icims.com",
	"careers-learnacademy.icims.com",
	"careers-learnbehavioral.icims.com",
	"careers-leftfieldlabs.icims.com",
	"careers-legacyac.icims.com",
	"careers-legacyinc.icims.com",
	"careers-legacyscs.icims.com",
	"careers-leons.icims.com",
	"careers-levelupllc.icims.com",
	"careers-leye.icims.com",
	"careers-lhbcorp.icims.com",
	"careers-lhs.icims.com",
	"careers-libertycompaniesllc.icims.com",
	"careers-lifelinccorp.icims.com",
	"careers-lindstromair.icims.com",
	"careers-linesight.icims.com",
	"careers-liro.icims.com",
	"careers-livelmh.icims.com",
	"careers-llajobs.icims.com",
	"careers-lmi.icims.com",
	"careers-loancareservicing.icims.com",
	"careers-locaria.icims.com",
	"careers-logixbanking.icims.com",
	"careers-loretto2.icims.com",
	"careers-lorienhealth.icims.com",
	"careers-lowcountrypt.icims.com",
	"careers-lowesfoods.icims.com",
	"careers-lrp.icims.com",
	"careers-lrri.icims.com",
	"careers-lsac.icims.com",
	"careers-lspower.icims.com",
	"careers-lucky.icims.com",
	"careers-luriechildrens.icims.com",
	"careers-lw.icims.com",
	"careers-lynker.icims.com",
	"careers-macalester.icims.com",
	"careers-macny.icims.com",
	"careers-madeiramadeira.icims.com",
	"careers-mafint.icims.com",
	"careers-magaero.icims.com",
	"careers-magnera.icims.com",
	"careers-makingspace.icims.com",
	"careers-malanai.icims.com",
	"careers-mambu.icims.com",
	"careers-mana.icims.com",
	"careers-mancon.icims.com",
	"careers-manolani.icims.com",
	"careers-marathonfund.icims.com",
	"careers-marinclinic.icims.com",
	"careers-marinerfinance.icims.com",
	"careers-markon.icims.com",
	"careers-massywoodltd.icims.com",
	"careers-matchmg.icims.com",
	"careers-maxor.icims.com",
	"careers-mbpce.icims.com",
	"careers-mccarthyca.icims.com",
	"careers-mccurrach.icims.com",
	"careers-mcel.icims.com",
	"careers-mci.icims.com",
	"careers-mci2.icims.com",
	"careers-mcics.icims.com",
	"careers-mdvip.icims.com",
	"careers-mercuryinsurance.icims.com",
	"careers-meridianbioscience.icims.com",
	"careers-mesabaheating.icims.com",
	"careers-metabolon.icims.com",
	"careers-mfp.icims.com",
	"careers-mg2.icims.com",
	"careers-mgocpa.icims.com",
	"careers-michfb.icims.com",
	"careers-michiganfirst.icims.com",
	"careers-middlesexcountynj.icims.com",
	"careers-millgroupinc.icims.com",
	"careers-miltoncat.icims.com",
	"careers-mineralstech.icims.com",
	"careers-mjengineers.icims.com",
	"careers-mlssoccer.icims.com",
	"careers-mobiledentists.icims.com",
	"careers-montaguevans.icims.com",
	"careers-monterosatx.icims.com",
	"careers-morganplc.icims.com",
	"careers-morrisjenkins.icims.com",
	"careers-mountainhomeservices.icims.com",
	"careers-mpi.icims.com",
	"careers-mpisystems.icims.com",
	"careers-mpowerpractice.icims.com",
	"careers-mpr.icims.com",
	"careers-mrplumberindy.icims.com",
	"careers-msa-ps.icims.com",
	"careers-mtfbiologics.icims.com",
	"careers-mtlcraft.icims.com",
	"careers-muckleshootgov.icims.com",
	"careers-mwdh2o.icims.com",
	"careers-myacuity.icims.com",
	"careers-mydental.icims.com",
	"careers-mysagedental.icims.com",
	"careers-na-merlinentertainments.icims.com",
	"careers-nacg.icims.com",
	"careers-nadentalgroup.icims.com",
	"careers-nafinc.icims.com",
	"careers-nahealth.icims.com",
	"careers-nakupuna.icims.com",
	"careers-naphcare.icims.com",
	"careers-nassusa-epsii.icims.com",
	"careers-natdcp.icims.com",
	"careers-nathealthcare.icims.com",
	"careers-nationaldebtrelief.icims.com",
	"careers-nautica.icims.com",
	"careers-navanta.icims.com",
	"careers-navitas.icims.com",
	"careers-navitus.icims.com",
	"careers-ncfe.icims.com",
	"careers-nelsonmullins.icims.com",
	"careers-netherlands-magnera.icims.com",
	"careers-netimpactstrategies.icims.com",
	"careers-newhaven.icims.com",
	"careers-nextechna.icims.com",
	"careers-nikkiso.icims.com",
	"careers-nipponexpress.icims.com",
	"careers-nl-kiwa.icims.com",
	"careers-nl.icims.com",
	"careers-nmotiontherapy.icims.com",
	"careers-nomadfoods.icims.com",
	"careers-northerncolorado-ymca.icims.com",
	"careers-northside.icims.com",
	"careers-novelis.icims.com",
	"careers-noven.icims.com",
	"careers-novolex.icims.com",
	"careers-nrdc.icims.com",
	"careers-nrgmr.icims.com",
	"careers-nscglobal.icims.com",
	"careers-ntpc.icims.com",
	"careers-ntrepidcorp.icims.com",
	"careers-nuggetmarket.icims.com",
	"careers-nv5.icims.com",
	"careers-nvisioncenters.icims.com",
	"careers-nybloodcenter.icims.com",
	"careers-nyfoundling.icims.com",
	"careers-nyit.icims.com",
	"careers-obmc.icims.com",
	"careers-obxtek.icims.com",
	"careers-oceanbank.icims.com",
	"careers-odysseyconsult.icims.com",
	"careers-oefederal.icims.com",
	"careers-oldnational.icims.com",
	"careers-omegatechserv.icims.com",
	"careers-omnisource.icims.com",
	"careers-oneadvanced.icims.com",
	"careers-oneidentity.icims.com",
	"careers-orange.icims.com",
	"careers-orientaltrading.icims.com",
	"careers-origamirisk.icims.com",
	"careers-osagecasino.icims.com",
	"careers-osuphysicians.icims.com",
	"careers-otterproducts.icims.com",
	"careers-ovg.icims.com",
	"careers-ovgcan.icims.com",
	"careers-ovguk.icims.com",
	"careers-pactworld.icims.com",
	"careers-palmettocitizens.icims.com",
	"careers-palmlakecareers.icims.com",
	"careers-pamhealth.icims.com",
	"careers-papaliaplumbing.icims.com",
	"careers-paperworksindustries.icims.com",
	"careers-paramedicbilling.icims.com",
	"careers-parkerandsons.icims.com",
	"careers-partnershiphp.icims.com",
	"careers-partsauthority.icims.com",
	"careers-patternenergy.icims.com",
	"careers-pctedu.icims.com",
	"careers-pdh.icims.com",
	"careers-peagroup.icims.com",
	"careers-peelregion.icims.com",
	"careers-pennex.icims.com",
	"careers-peoria-ymca.icims.com",
	"careers-pepperpointe.icims.com",
	"careers-persistentsystems.icims.com",
	"careers-personifyhealth.icims.com",
	"careers-petsuppliesplus.icims.com",
	"careers-pgworks.icims.com",
	"careers-phc.icims.com",
	"careers-pickettusa.icims.com",
	"careers-pictsweet.icims.com",
	"careers-pistongroup.icims.com",
	"careers-plannedparenthood.icims.com",
	"careers-plansys.icims.com",
	"careers-platinum.icims.com",
	"careers-plazatire.icims.com",
	"careers-plslogistics.icims.com",
	"careers-plumblineservices.icims.com",
	"careers-pne.icims.com",
	"careers-polkcountysheriffsoffice.icims.com",
	"careers-posillicoinc.icims.com",
	"careers-postacute-affiliates.icims.com",
	"careers-powerschool.icims.com",
	"careers-ppl.icims.com",
	"careers-pragermetis.icims.com",
	"careers-praxispackaging.icims.com",
	"careers-precisiontoday.icims.com",
	"careers-premierbuildingsupply.icims.com",
	"careers-premierdentistpartners.icims.com",
	"careers-premierehcstaffing.icims.com",
	"careers-preshomes.icims.com",
	"careers-prg.icims.com",
	"careers-prginternational.icims.com",
	"careers-pridemechanicalllc.icims.com",
	"careers-primecontrols.icims.com",
	"careers-primeinc.icims.com",
	"careers-prismvisiongroup.icims.com",
	"careers-propark.icims.com",
	"careers-proudmomentsaba.icims.com",
	"careers-psaairlines.icims.com",
	"careers-psi.icims.com",
	"careers-pst.icims.com",
	"careers-ptsolutions.icims.com",
	"careers-publiccounsel.icims.com",
	"careers-pulice.icims.com",
	"careers-pvm.icims.com",
	"careers-pwrteams.icims.com",
	"careers-pyramidsystems.icims.com",
	"careers-qchek.icims.com",
	"careers-qinetiqus.icims.com",
	"careers-ql.icims.com",
	"careers-quanta.icims.com",
	"careers-quickcallswick.icims.com",
	"careers-quinngroup.icims.com",
	"careers-ragsdaleair.icims.com",
	"careers-rambus.icims.com",
	"careers-randgroup.icims.com",
	"careers-rare.icims.com",
	"careers-reachire.icims.com",
	"careers-realpagepms.icims.com",
	"careers-redcanyonpt.icims.com",
	"careers-redcoats.icims.com",
	"careers-reisystems.icims.com",
	"careers-relfm.icims.com",
	"careers-reliant-rehab.icims.com",
	"careers-renasant.icims.com",
	"careers-resonetics.icims.com",
	"careers-resortsac.icims.com",
	"careers-reynoldsconsumerproducts.icims.com",
	"careers-ribc.icims.com",
	"careers-ricardo.icims.com",
	"careers-rightnowheatingacplumbing.icims.com",
	"careers-ringpower.icims.com",
	"careers-riverbed.icims.com",
	"careers-rivercree.icims.com",
	"careers-riversidehealth.icims.com",
	"careers-riversideresearch.icims.com",
	"careers-rivet.icims.com",
	"careers-riyadhair.icims.com",
	"careers-rmrgroup.icims.com",
	"careers-roadrunnersports.icims.com",
	"careers-robertgraham.icims.com",
	"careers-robertson.icims.com",
	"careers-rochebros.icims.com",
	"careers-rocketpharma.icims.com",
	"careers-rockheating.icims.com",
	"careers-rockwood.icims.com",
	"careers-rosepestsolutions.icims.com",
	"careers-rovisys.icims.com",
	"careers-rovisyseurope.icims.com",
	"careers-rp-techologies.icims.com",
	"careers-rpmliving.icims.com",
	"careers-rrsc.icims.com",
	"careers-rsandh.icims.com",
	"careers-rssb.icims.com",
	"careers-rugdoctor.icims.com",
	"careers-ruralking.icims.com",
	"careers-rvu.icims.com",
	"careers-rws.icims.com",
	"careers-rydon.icims.com",
	"careers-saft.icims.com",
	"careers-sagaftra.icims.com",
	"careers-sagehospitality.icims.com",
	"careers-salemmedia.icims.com",
	"careers-salemsurround.icims.com",
	"careers-samaritanvillage.icims.com",
	"careers-sanctuaryhospice.icims.com",
	"careers-sares-regis.icims.com",
	"careers-sargentlundy.icims.com",
	"careers-sarh.icims.com",
	"careers-sas.icims.com",
	"careers-sazerac.icims.com",
	"careers-sbec.icims.com",
	"careers-scatec.icims.com",
	"careers-sciolex.icims.com",
	"careers-scires.icims.com",
	"careers-screwfix.icims.com",
	"careers-sdilafarga.icims.com",
	"careers-sdsurf.icims.com",
	"careers-securityusainc.icims.com",
	"careers-sedanos.icims.com",
	"careers-selux.icims.com",
	"careers-sentinel.icims.com",
	"careers-servicechampions.icims.com",
	"careers-sesi-md.icims.com",
	"careers-sev1tech.icims.com",
	"careers-sevenhills.icims.com",
	"careers-shamal.icims.com",
	"careers-shawmut.icims.com",
	"careers-shepleywoodproducts.icims.com",
	"careers-shimmick.icims.com",
	"careers-shionogi.icims.com",
	"careers-shure.icims.com",
	"careers-sig.icims.com",
	"careers-signethealth.icims.com",
	"careers-sil.icims.com",
	"careers-silveredgegs.icims.com",
	"careers-silvi.icims.com",
	"careers-simpsonhousing.icims.com",
	"careers-simventions.icims.com",
	"careers-sisk.icims.com",
	"careers-skdk.icims.com",
	"careers-skh.icims.com",
	"careers-sklifescienceinc.icims.com",
	"careers-skyclimber.icims.com",
	"careers-slco.icims.com",
	"careers-smileamericapartners.icims.com",
	"careers-smilenyoutreach.icims.com",
	"careers-smyrnareadymix.icims.com",
	"careers-snapon.icims.com",
	"careers-softtechconsulting.icims.com",
	"careers-soitec.icims.com",
	"careers-solairus.icims.com",
	"careers-somniainc.icims.com",
	"careers-sonalysts.icims.com",
	"careers-sonrava.icims.com",
	"careers-sos-kd.icims.com",
	"careers-southcoast.icims.com",
	"careers-southend-borough-council.icims.com",
	"careers-southland.icims.com",
	"careers-souto.icims.com",
	"careers-spain-magnera.icims.com",
	"careers-spanish-fsg.icims.com",
	"careers-spanish-redcoats.icims.com",
	"careers-spcco.icims.com",
	"careers-springswindowfashions.icims.com",
	"careers-srcx.icims.com",
	"careers-sri.icims.com",
	"careers-sscgp.icims.com",
	"careers-ssemc.icims.com",
	"careers-ssoe.icims.com",
	"careers-sss-steel.icims.com",
	"careers-standoutfieldmarketing.icims.com",
	"careers-stapharma.icims.com",
	"careers-stardental.icims.com",
	"careers-steampunk.icims.com",
	"careers-steeldynamics.icims.com",
	"careers-steinhafels.icims.com",
	"careers-stellarsolutions.icims.com",
	"careers-stevesplumbinghawaii.icims.com",
	"careers-stonybrookmedicinecpmp.icims.com",
	"careers-stotzequipment.icims.com",
	"careers-stratus.icims.com",
	"careers-structurepoint.icims.com",
	"careers-stuartmechanical.icims.com",
	"careers-suffolkconstruction.icims.com",
	"careers-sumter.icims.com",
	"careers-sunauto.icims.com",
	"careers-suncrestcare.icims.com",
	"careers-sundevil.icims.com",
	"careers-superior-ymca.icims.com",
	"careers-superiorindustrialfire.icims.com",
	"careers-supportuw.icims.com",
	"careers-sureway.icims.com",
	"careers-sus.icims.com",
	"careers-sv-kiwa.icims.com",
	"careers-svclnk.icims.com",
	"careers-svh.icims.com",
	"careers-swca.icims.com",
	"careers-swissport.icims.com",
	"careers-symbria.icims.com",
	"careers-symmons.icims.com",
	"careers-symplr.icims.com",
	"careers-sysmex.icims.com",
	"careers-tacten.icims.com",
	"careers-tactica-solutions.icims.com",
	"careers-talentcollab.icims.com",
	"careers-tatitlek.icims.com",
	"careers-tcnb.icims.com",
	"careers-tcw.icims.com",
	"careers-team-rehab.icims.com",
	"careers-tecequipment.icims.com",
	"careers-techcu.icims.com",
	"careers-technicalassociates.icims.com",
	"careers-teksynap.icims.com",
	"careers-teksystems.icims.com",
	"careers-teleperformance.icims.com",
	"careers-tenderrose.icims.com",
	"careers-terso.icims.com",
	"careers-texasbar.icims.com",
	"careers-tfghospitality.icims.com",
	"careers-tgkauto.icims.com",
	"careers-thefirstgroup.icims.com",
	"careers-thekeycan.icims.com",
	"careers-thenannyleague.icims.com",
	"careers-thesilverlining.icims.com",
	"careers-thestrongholdcompanies.icims.com",
	"careers-thesydneycentre.icims.com",
	"careers-thinktogether.icims.com",
	"careers-thomasgalbraith.icims.com",
	"careers-tier1.icims.com",
	"careers-tierpoint.icims.com",
	"careers-tighebond.icims.com",
	"careers-tiremaxnc.icims.com",
	"careers-titanmachinery.icims.com",
	"careers-tlcconstruction.icims.com",
	"careers-tls.icims.com",
	"careers-tmh.icims.com",
	"careers-tmo.icims.com",
	"careers-togetherwomenshealth.icims.com",
	"careers-topco.icims.com",
	"careers-toscalito.icims.com",
	"careers-trademarkmechanical.icims.com",
	"careers-tradeupcaliforniaregion.icims.com",
	"careers-transcat.icims.com",
	"careers-transmed.icims.com",
	"careers-treliant.icims.com",
	"careers-trimac.icims.com",
	"careers-tritonair.icims.com",
	"careers-tsne.icims.com",
	"careers-tsoln-inc.icims.com",
	"careers-tuality.icims.com",
	"careers-turn5.icims.com",
	"careers-tutera.icims.com",
	"careers-tyndaleusa.icims.com",
	"careers-uabmedicine.icims.com",
	"careers-ucbi.icims.com",
	"careers-ucpcentralpa.icims.com",
	"careers-uewhealth.icims.com",
	"careers-uhnjcareers.icims.com",
	"careers-uk-merlinentertainments.icims.com",
	"careers-uknordicswitz-berkley.icims.com",
	"careers-undergroundconstruction.icims.com",
	"careers-unfcu.icims.com",
	"careers-unisonglobal.icims.com",
	"careers-universalhealthservices.icims.com",
	"careers-upbring.icims.com",
	"careers-urbanscience.icims.com",
	"careers-urs.icims.com",
	"careers-us-secure.icims.com",
	"careers-us-shermco.icims.com",
	"careers-us-vistaglobal.icims.com",
	"careers-usa-cecoenviro.icims.com",
	"careers-usa-titanmaterials.icims.com",
	"careers-uscargo.icims.com",
	"careers-usesalvationarmy.icims.com",
	"careers-usfintech.icims.com",
	"careers-usonainstitute.icims.com",
	"careers-usopen.icims.com",
	"careers-usptm.icims.com",
	"careers-usta.icims.com",
	"careers-usu.icims.com",
	"careers-uti.icims.com",
	"careers-uuhc.icims.com",
	"careers-uwcu.icims.com",
	"careers-uwmcareers.icims.com",
	"careers-v-nova.icims.com",
	"careers-vailgov.icims.com",
	"careers-valiantsolutions.icims.com",
	"careers-valorvip.icims.com",
	"careers-vanir.icims.com",
	"careers-vantageairportgroup.icims.com",
	"careers-vch.icims.com",
	"careers-ventrahealth.icims.com",
	"careers-vergemobilellc.icims.com",
	"careers-vet.icims.com",
	"careers-vetcor.icims.com",
	"careers-veteranair.icims.com",
	"careers-vettech.icims.com",
	"careers-vhb.icims.com",
	"careers-vhchealth.icims.com",
	"careers-viapath.icims.com",
	"careers-vinfen.icims.com",
	"careers-viseriongrain.icims.com",
	"careers-visioninnovation-partners.icims.com",
	"careers-vistaamerica.icims.com",
	"careers-vistaglobal.icims.com",
	"careers-vistrygroup.icims.com",
	"careers-voancnn.icims.com",
	"careers-vocationaldevelopmentgroupllc.icims.com",
	"careers-vtr.icims.com",
	"careers-wafd.icims.com",
	"careers-wakefield-cecoenviro.icims.com",
	"careers-wakely.icims.com",
	"careers-walbridge.icims.com",
	"careers-walterpmoore.icims.com",
	"careers-warhorsecasino.icims.com",
	"careers-warrenhenryauto.icims.com",
	"careers-waterway.icims.com",
	"careers-wbs.icims.com",
	"careers-wcpss.icims.com",
	"careers-weareteam.icims.com",
	"careers-wel.icims.com",
	"careers-wellbeseniormedical.icims.com",
	"careers-wellingtonsteele.icims.com",
	"careers-werfen.icims.com",
	"careers-westcoastuniversity.icims.com",
	"careers-westernmilling.icims.com",
	"careers-westernsouthern.icims.com",
	"careers-wginc.icims.com",
	"careers-wheelsup.icims.com",
	"careers-whippleservicechampions.icims.com",
	"careers-wilhelm.icims.com",
	"careers-willowvalleycommunities.icims.com",
	"careers-wilson.icims.com",
	"careers-winco.icims.com",
	"careers-winshape.icims.com",
	"careers-wipfli.icims.com",
	"careers-wkgermany-cecoenviro.icims.com",
	"careers-wleeflowers.icims.com",
	"careers-wow.icims.com",
	"careers-wseinc.icims.com",
	"careers-wth.icims.com",
	"careers-wuxiapptec.icims.com",
	"careers-ymcasd.icims.com",
	"careers-ymmc.icims.com",
	"careers-yokohamatire.icims.com",
	"careers-yptc.icims.com",
	"careers-yugo.icims.com",
	"careers-yusa-ymca.icims.com",
	"careers-yusen-logistics.icims.com",
	"careers-zemitek.icims.com",
	"careers-zenova.icims.com",
	"careers13-oremorautomotive.icims.com",
	"careers19-oremorautomotive.icims.com",
	"careers2-aegisliving.icims.com",
	"careers2-anothersource.icims.com",
	"careers2-blanchard.icims.com",
	"careers2-carle.icims.com",
	"careers2-columbuszoo.icims.com",
	"careers2-extensishr.icims.com",
	"careers2-eyesouthpartners.icims.com",
	"careers2-imagefirst.icims.com",
	"careers2-knipper.icims.com",
	"careers2-oremorautomotive.icims.com",
	"careers2-quanta.icims.com",
	"careers2-tradesmen.icims.com",
	"careers2-universalhealthservices.icims.com",
	"careers2-vistaequitypartners.icims.com",
	"careers2-wyn.icims.com",
	"careers3-powerschool.icims.com",
	"careers4-calamp.icims.com",
	"careers6-oremorautomotive.icims.com",
	"careersbroadwaysupportservices-nationaldebtrelief.icims.com",
	"careersco-teleperformance.icims.com",
	"careersen-baffinland.icims.com",
	"careersen-fountaintire.icims.com",
	"careersen-hrrh.icims.com",
	"careersen-mackenzieinvestments.icims.com",
	"careersen-sircorp.icims.com",
	"careersen-snackruptors.icims.com",
	"careersen-victoria.icims.com",
	"careerseng-senture.icims.com",
	"careerseng-teleperformance.icims.com",
	"careersenus-fgfbrands.icims.com",
	"careersger-itt-inc.icims.com",
	"careersintern-us-itt-inc.icims.com",
	"careersintl-maxlinear.icims.com",
	"careersonland-starboardcruise.icims.com",
	"careersph-teleperformance.icims.com",
	"careersuk-integreon.icims.com",
	"careersus-endologix.icims.com",
	"careersus-maxlinear.icims.com",
	"careersus-shure.icims.com",
	"careersus-teleperformance.icims.com",
	"caribbean-bermudajobs-ajg.icims.com",
	"caribbean-envoyair.icims.com",
	"carolinapolyenglish-poly-america.icims.com",
	"carreras-gildan.icims.com",
	"carreras-hrl-gildan.icims.com",
	"carrieres-axionefrance.icims.com",
	"carrieres-fr-marvestingbe.icims.com",
	"carrieres-fr-promosapiens.icims.com",
	"carrieres-marvesting.icims.com",
	"carrieres-nl-marvestingbe.icims.com",
	"carrieres-nl-promosapiens.icims.com",
	"ccssw-ccsww.icims.com",
	"cds-canada-careersenglish.icims.com",
	"cds-canada-careersfrench.icims.com",
	"centraloffice-scsk12.icims.com",
	"centrikidsummerstaff-lifeway.icims.com",
	"ch-external-novelis.icims.com",
	"chesapeakeva-allanmyers.icims.com",
	"circetusa-kgpco.icims.com",
	"citizenscareers-ahrc.icims.com",
	"city-boston.icims.com",
	"classified-ocps.icims.com",
	"clients-eidebailly.icims.com",
	"clinical-pediatrix.icims.com",
	"clinical-usap.icims.com",
	"clinicalexternal-athletico.icims.com",
	"clubcareers-planetfitness.icims.com",
	"clubjobs-bgca.icims.com",
	"communities-tutera.icims.com",
	"community-jerseystem.icims.com",
	"communitycare-centralhealth.icims.com",
	"communitysupport-atria.icims.com",
	"contractor-careers-carle.icims.com",
	"contractwork-eauditstaff.icims.com",
	"conversion-ymmc.icims.com",
	"corporate-nationalbeef.icims.com",
	"corporatecareers-luckystrikeentertainment.icims.com",
	"corporatecareers-planetfitness.icims.com",
	"corporatecareers-thefreshmarket.icims.com",
	"courtsandgrounds-usta.icims.com",
	"creeksideterracerehab-careers-fundltc.icims.com",
	"cretexmedicalcdt-cretex.icims.com",
	"crew-jkmoving.icims.com",
	"dcacareers-dentalcarealliance.icims.com",
	"de-careers-novelis.icims.com",
	"de-truesociety.icims.com",
	"dentalcareers-mb2dental.icims.com",
	"dentistcareers-nadentalgroup.icims.com",
	"devlinmanornursingandrehab-careers-fundltc.icims.com",
	"dimatixcareers-fujifilm.icims.com",
	"doctor-beaumonthospital.icims.com",
	"drive4us-commtrans.icims.com",
	"driver-tech-casella.icims.com",
	"drivercareers-adspipe.icims.com",
	"drivers-natdcp.icims.com",
	"drybarshops-wellbizbrands.icims.com",
	"element-ext-fr.icims.com",
	"element-ext-ger.icims.com",
	"element-ext-korea.icims.com",
	"element-ext-row.icims.com",
	"element-ext-us.icims.com",
	"elementsmassage-wellbizbrands.icims.com",
	"emea-apj-riverbed.icims.com",
	"emea-blackhawknetwork.icims.com",
	"emea-cookmedical.icims.com",
	"emea-external-aitworldwide.icims.com",
	"emeacareers-lumanity.icims.com",
	"emploi-castorama.icims.com",
	"employment-mackenziehealth.icims.com",
	"encareers-cmh.icims.com",
	"enfrench-equans.icims.com",
	"english-poly-america.icims.com",
	"english-stonex.icims.com",
	"englishcareers-bartlett.icims.com",
	"eps-careers-epsii.icims.com",
	"escareers-campero.icims.com",
	"europe-geosyntec.icims.com",
	"everwatch-everwatchsolutions.icims.com",
	"experienced-toyota-europe.icims.com",
	"expleo-jobs-be-en.icims.com",
	"expleo-jobs-ch-en.icims.com",
	"expleo-jobs-de-de.icims.com",
	"expleo-jobs-eg-en.icims.com",
	"expleo-jobs-es-en.icims.com",
	"expleo-jobs-fr-fr.icims.com",
	"expleo-jobs-gb-en.icims.com",
	"expleo-jobs-ie-en.icims.com",
	"expleo-jobs-in-en.icims.com",
	"expleo-jobs-ma-fr.icims.com",
	"expleo-jobs-nl-en.icims.com",
	"expleo-jobs-pt-en.icims.com",
	"expleo-jobs-za-en.icims.com",
	"external-92y.icims.com",
	"external-ascendfcu.icims.com",
	"external-brandhouseco.icims.com",
	"external-canoncareers.icims.com",
	"external-careers-sodexomagic.icims.com",
	"external-cbha.icims.com",
	"external-dillon.icims.com",
	"external-fladarchitects.icims.com",
	"external-flysfo.icims.com",
	"external-generalrv.icims.com",
	"external-goaheadlondon.icims.com",
	"external-minco.icims.com",
	"external-pccsea.icims.com",
	"external-penskemotorgroup.icims.com",
	"external-saintmarysuniversity.icims.com",
	"external-stryten.icims.com",
	"external-telecom-teldta.icims.com",
	"externalhourly-innventures.icims.com",
	"externalhourly-kessler.icims.com",
	"externalhourly-omnihotels.icims.com",
	"externalhourly-viceroy.icims.com",
	"externalsp-spplus.icims.com",
	"faculty-childrensmercykc.icims.com",
	"faculty-clarkson.icims.com",
	"faculty-saintmarysuniversity.icims.com",
	"facultyemployment-stthomas.icims.com",
	"fairfieldhealthcenter-careers-fundltc.icims.com",
	"fbh-ccsww.icims.com",
	"field-english-danos.icims.com",
	"field-hourlycareers-luckystrikeentertainment.icims.com",
	"field-mvtransit.icims.com",
	"fieldhourly-thefreshmarket.icims.com",
	"financialadvisorcareers-captrust.icims.com",
	"fitnesstogether-wellbizbrands.icims.com",
	"floridadetroitdieselallison-kirbycorp.icims.com",
	"fmmcareer-epikafleet.icims.com",
	"fox-rollins.icims.com",
	"france-erac.icims.com",
	"frcareers-lw.icims.com",
	"freedompt-tptp.icims.com",
	"french-canadian-gd-ots.icims.com",
	"french-external-prodwaredag.icims.com",
	"frenchexternal-mccarthyca.icims.com",
	"fugesummerstaff-lifeway.icims.com",
	"futurecareers-peopletec.icims.com",
	"general-beaumonthospital.icims.com",
	"georgiaexternal-mobisalabamallc.icims.com",
	"ger-erac.icims.com",
	"gercareers-horton.icims.com",
	"germancareers-bruker.icims.com",
	"global-nyu.icims.com",
	"global-portal-ttelectronics.icims.com",
	"globalcareers-cotiviti.icims.com",
	"globalcareers-granicus.icims.com",
	"globalcareers-mayerbrown.icims.com",
	"globalcareers-msci.icims.com",
	"globalcareers-yelp.icims.com",
	"grad-chire.icims.com",
	"graduate-toyota-europe.icims.com",
	"greenvalleyrehabsuites-careers-fundltc.icims.com",
	"harmonmedicalrehab-careers-fundltc.icims.com",
	"healthcare-diversicare.icims.com",
	"healthcareathomejobs-en.icims.com",
	"hgcareers-harndengroup.icims.com",
	"hmi-esp-harvard.icims.com",
	"holiday-frontlinewagedisplay.icims.com",
	"homeoffice-eu-urbn.icims.com",
	"homeoffice-na-urbn.icims.com",
	"horizonhealthandrehab-careers-fundltc.icims.com",
	"horizonspecialtyhosp-careers-fundltc.icims.com",
	"hospital-midlandhealth.icims.com",
	"hourly-canada-redlobster.icims.com",
	"hourly-careers-waterway.icims.com",
	"hourly-fotlinc.icims.com",
	"hourly-greatdane.icims.com",
	"hourly-nationalbeef.icims.com",
	"hourly-spanish-redlobster.icims.com",
	"hourlycareers-na-merlinentertainments.icims.com",
	"hourlyjobs-pushingdaisies.icims.com",
	"hq-lifeway.icims.com",
	"hqcareers-indevets.icims.com",
	"htccareers-touro.icims.com",
	"hub-weareavidity.icims.com",
	"idccareers-apac-idg.icims.com",
	"idccareers-canada-idg.icims.com",
	"idccareers-emea-idg.icims.com",
	"idccareers-idg.icims.com",
	"idccareers-latam-idg.icims.com",
	"ideas-sas.icims.com",
	"indeedapply-suburbanpropane.icims.com",
	"india-external-dish.icims.com",
	"indiacareers-symplr.icims.com",
	"instructional-scsk12.icims.com",
	"intern-careers-ovg.icims.com",
	"international-daddario.icims.com",
	"international-sargentlundy.icims.com",
	"internationalcareers-waters.icims.com",
	"internships-aei.icims.com",
	"intl-careers-verathon.icims.com",
	"ire-erac.icims.com",
	"it-careers-novelis.icims.com",
	"it-emeacareers-trinseo.icims.com",
	"iwtcareers-ads-pipe.icims.com",
	"jamcareers-ibex.icims.com",
	"jobs-accion.icims.com",
	"jobs-alpa.icims.com",
	"jobs-auxis.icims.com",
	"jobs-b3h.icims.com",
	"jobs-bahs.icims.com",
	"jobs-barnard-inc.icims.com",
	"jobs-bylight.icims.com",
	"jobs-ccoc.icims.com",
	"jobs-centerlinelogistics.icims.com",
	"jobs-centralmarket.icims.com",
	"jobs-cesi.icims.com",
	"jobs-challp.icims.com",
	"jobs-childrensmercykc.icims.com",
	"jobs-choosememorial.icims.com",
	"jobs-daa.icims.com",
	"jobs-donohoe.icims.com",
	"jobs-edgewoodproperties.icims.com",
	"jobs-express.icims.com",
	"jobs-fchp.icims.com",
	"jobs-firebirdsrestaurants.icims.com",
	"jobs-getty.icims.com",
	"jobs-hopeinternational.icims.com",
	"jobs-iamempowered.icims.com",
	"jobs-jhpiego.icims.com",
	"jobs-joevsmartshop.icims.com",
	"jobs-joslin.icims.com",
	"jobs-kennedykrieger.icims.com",
	"jobs-lamothermic.icims.com",
	"jobs-lg-tek.icims.com",
	"jobs-medcura.icims.com",
	"jobs-metagenics.icims.com",
	"jobs-middlesexsavings.icims.com",
	"jobs-myvschome.icims.com",
	"jobs-novamedical.icims.com",
	"jobs-openskycs.icims.com",
	"jobs-placeme.icims.com",
	"jobs-popshelf.icims.com",
	"jobs-precisionsolutions.icims.com",
	"jobs-rainbird.icims.com",
	"jobs-rbscorp.icims.com",
	"jobs-richdalegroup.icims.com",
	"jobs-roomandboard.icims.com",
	"jobs-sdgoodwill.icims.com",
	"jobs-selectmedicalcorp.icims.com",
	"jobs-sierralobo.icims.com",
	"jobs-tollbrothers.icims.com",
	"jobs-townandcountrymarkets.icims.com",
	"jobs-tradesmen.icims.com",
	"jobs-trustmark.icims.com",
	"jobs-valeris.icims.com",
	"jobs-veteransprime.icims.com",
	"jobs1-spectrumhealthcare.icims.com",
	"jobsamericas-gep.icims.com",
	"jobseurope-gep.icims.com",
	"jobsindia-apac-gep.icims.com",
	"jobsus-gep.icims.com",
	"joesautoparks-tlrgc.icims.com",
	"juliamanornursingandrehab-careers-fundltc.icims.com",
	"karriere-prodwaredag.icims.com",
	"kimley-horndc.icims.com",
	"kineticptinstitute-tptp.icims.com",
	"kphcareers-kphhealthcareservices.icims.com",
	"kuna-nana.icims.com",
	"lakecityscrantonhc-careers-fundltc.icims.com",
	"lakeemorypostacutecare-careers-fundltc.icims.com",
	"lancasterhealthcenter-careers-fundltc.icims.com",
	"lightstreetspecialeducation-learnbehavioral.icims.com",
	"littleleaves-learnbehavioral.icims.com",
	"lksomphysiciancareers-temple.icims.com",
	"magnoliamanorspartanburg-careers-fundltc.icims.com",
	"main-princeton.icims.com",
	"maintenance-lincolnapts2.icims.com",
	"managedmobilecareers-epikafleet.icims.com",
	"management-canada-redlobster.icims.com",
	"management-davidsonhospitality.icims.com",
	"management-millersalehouse.icims.com",
	"managementcareers-luckystrikeentertainment.icims.com",
	"managementcareers-missionlinen.icims.com",
	"marinesystems-kirbycorp.icims.com",
	"maritimecareers-kirbycorp.icims.com",
	"mdcareers-livech.icims.com",
	"menusandvenues-na-urbn.icims.com",
	"mexicocareers-prodrivenbrands.icims.com",
	"mexicocareers-shriners.icims.com",
	"midlandsbehavioralhealth-careers-fundltc.icims.com",
	"militaryjobs-mci.icims.com",
	"miscareers-kellerna.icims.com",
	"nationalalamo-erac.icims.com",
	"nationalalamocafr-erac.icims.com",
	"netherlands-careers-aitworldwide.icims.com",
	"non-clinical-emory.icims.com",
	"non-clinical-pediatrix.icims.com",
	"nonacademiccareers-udst.icims.com",
	"northamptonnursingandrehab-careers-fundltc.icims.com",
	"nurses-beaumonthospital.icims.com",
	"nursing-thekey.icims.com",
	"nusourcellccareers-mpr.icims.com",
	"nymccareers-touro.icims.com",
	"oakbrookhealthcenter-careers-fundltc.icims.com",
	"office-flaggerforce.icims.com",
	"operationcareers-westernsouthern.icims.com",
	"osldirectcareersfr-oslrs.icims.com",
	"outsourcingcareers-omniemployment.icims.com",
	"parks-jocogov.icims.com",
	"pavilionatcreekwood-careers-fundltc.icims.com",
	"pdsh-da-pdshealth.icims.com",
	"phcareers-qualfon.icims.com",
	"phlcareers-livech.icims.com",
	"physicians-baycaremedicalgroup.icims.com",
	"physicians-cooperhealth.icims.com",
	"pillarcareers-beitzelpillar.icims.com",
	"polywestenglish-poly-america.icims.com",
	"porter-oneworkplace.icims.com",
	"pppl-princeton.icims.com",
	"pr-erac.icims.com",
	"prague-careers-aitworldwide.icims.com",
	"prioritiesaba-learnbehavioral.icims.com",
	"professional-allanmyers.icims.com",
	"professional-headfirstcamps.icims.com",
	"professionalcareers-analysisgroup.icims.com",
	"provider-lhs.icims.com",
	"providercareers-bsahealth.icims.com",
	"providercareers-harkerheights.icims.com",
	"providercareers-hillcresthealth.icims.com",
	"providercareers-lovelacehealth.icims.com",
	"providercareers-mountainsidemedical.icims.com",
	"providercareers-pascackvalley.icims.com",
	"providercareers-portneuf.icims.com",
	"providercareers-universityofkansas.icims.com",
	"providercareers-uthealth.icims.com",
	"providers-dartmouth-hitchcock.icims.com",
	"prwcspartanburg-careers-fundltc.icims.com",
	"pt-careers-hovione.icims.com",
	"pt-careers-novelis.icims.com",
	"qts-cretex.icims.com",
	"radiantwaxing-wellbizbrands.icims.com",
	"readingisfundamental-puzzlehr.icims.com",
	"reborncareers-renovo.icims.com",
	"reconservices-careers-site.icims.com",
	"recovery-ampact.icims.com",
	"recrute1-carrefour.icims.com",
	"renewables-skyclimber.icims.com",
	"restaurantsupport-millersalehouse.icims.com",
	"restorerehabilitationcenter-careers-fundltc.icims.com",
	"resume-chesterton.icims.com",
	"retail-shaneco.icims.com",
	"retailcareers-vitaminshoppe.icims.com",
	"rms-cretex.icims.com",
	"rtihs-rtiinc.icims.com",
	"salescareers-westernsouthern.icims.com",
	"sangabrielrehabcenter-careers-fundltc.icims.com",
	"savannahexternal-mobisalabamallc.icims.com",
	"searchcareers-omniemployment.icims.com",
	"seasonalmlb-headfirstcamps.icims.com",
	"service-princeton.icims.com",
	"servicesite-ampact.icims.com",
	"shanghai-nyu.icims.com",
	"shorecareers-kirbycorp.icims.com",
	"southpointehealthandrehab-careers-fundltc.icims.com",
	"spain-erac.icims.com",
	"spanish-poly-america.icims.com",
	"spanishhillswellness-careers-fundltc.icims.com",
	"springdalehealthcarecenter-careers-fundltc.icims.com",
	"staff-cua.icims.com",
	"staffemployment-stthomas.icims.com",
	"sterlingoaksrehab-careers-fundltc.icims.com",
	"stewartstevenson-kirbycorp.icims.com",
	"stirlingdynamics-jobs-expleo.icims.com",
	"storecareers-gpminvestments.icims.com",
	"storejobs-qchek.icims.com",
	"stores-eu-urbn.icims.com",
	"stores-na-urbn.icims.com",
	"studentlife-lifeway.icims.com",
	"synergyptaw-tptp.icims.com",
	"tacareers-waters.icims.com",
	"tcnycareers-touro.icims.com",
	"teachers-ocps.icims.com",
	"teammember-millersalehouse.icims.com",
	"technician-titanmachinery.icims.com",
	"terrabellahealth-careers-fundltc.icims.com",
	"thefirstcollection-thefirstgroup.icims.com",
	"theracorept-tptp.icims.com",
	"thermoking-kirbycorp.icims.com",
	"totalspectrum-learnbehavioral.icims.com",
	"transportationservices-ketteringhealth.icims.com",
	"trellis-learnbehavioral.icims.com",
	"tuccareers-touro.icims.com",
	"uk-erac.icims.com",
	"uk-external-novelis.icims.com",
	"ukcareers-lw.icims.com",
	"ukjobs-mayerbrown.icims.com",
	"union-clarkson.icims.com",
	"upnorthenglish-poly-america.icims.com",
	"us-avisonyoung.icims.com",
	"us-careers-verathon.icims.com",
	"us-envoyair.icims.com",
	"us-erac.icims.com",
	"us-qualfon.icims.com",
	"usa-pursuitcollection.icims.com",
	"uscareeropenings-alliancelaundry.icims.com",
	"uscareers-acuren.icims.com",
	"uscareers-asm.icims.com",
	"uscareers-bruker.icims.com",
	"uscareers-fujifilm.icims.com",
	"uscareers-gildan.icims.com",
	"uscareers-hrl-gildan.icims.com",
	"uscareers-ibex.icims.com",
	"uscareers-idemia.icims.com",
	"uscareers-kellerna.icims.com",
	"uscareers-lewisenergy.icims.com",
	"uscareers-lumanity.icims.com",
	"uscareers-nyu.icims.com",
	"uscareers-oslrs.icims.com",
	"uscareers-repairify.icims.com",
	"uscareers-rws.icims.com",
	"uscareers-waters.icims.com",
	"uscareers-yelp.icims.com",
	"uscareers2-asm.icims.com",
	"uscareersnr-fujifilm.icims.com",
	"valleyfallsterrace-careers-fundltc.icims.com",
	"vendorrelations-rws.icims.com",
	"vertexcareers-atriumworks.icims.com",
	"villarosanursingandrehab-careers-fundltc.icims.com",
	"volunteer-alphaomegahospice.icims.com",
	"volunteer-altushospice.icims.com",
	"volunteer-brookhavenhospice.icims.com",
	"volunteer-continuumhospice.icims.com",
	"volunteer-dierksenhospice.icims.com",
	"volunteer-frontierhospice.icims.com",
	"volunteer-hospiceofchattanooga.icims.com",
	"volunteer-hospiceofthewest.icims.com",
	"volunteer-sanctuaryhospice.icims.com",
	"volunteerintern-marinclinic.icims.com",
	"volunteers-changegrowlive.icims.com",
	"volunteers-jerseystem.icims.com",
	"wallypark-tlrgc.icims.com",
	"walton-enterprises.icims.com",
	"warehouse-natdcp.icims.com",
	"wholesale-drivers-harborfoods.icims.com",
	"wholesale-harborfoods.icims.com",
	"wiltshirefarmfoods-apetito.icims.com",
	"zh-careers-novelis.icims.com",
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
