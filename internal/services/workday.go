package services

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin("workday", multiJobsFuncNamed(Workday, WorkdayCompanyURLs, workdayCompanyName))

	for _, companyURL := range WorkdayCompanyURLs {
		WorkdayCompanies = append(WorkdayCompanies, workdayCompanyName(companyURL))
	}
}

// WorkdayCompanies holds the company names derived from [WorkdayCompanyURLs].
// It is populated during package initialization.
var WorkdayCompanies []string

// workdayCompanyName derives a display name from a Workday tenant URL, by
// taking the first label of the host: given
// "https://pfizer.wd1.myworkdayjobs.com/PfizerCareers" it returns "pfizer".
//
// It returns the input unchanged if the URL cannot be parsed, so a malformed
// entry stays traceable back to its source rather than becoming an empty string.
func workdayCompanyName(companyURL string) string {
	u, err := url.Parse(companyURL)
	if err != nil || u.Hostname() == "" {
		return companyURL
	}

	host, _, _ := strings.Cut(u.Hostname(), ".")

	return host
}

// workdayCXSURL builds the "cxs" search endpoint that backs a Workday careers
// site, given the tenant host, tenant name, and the site path.
//
// The trailing "/jobs" is stripped from the site path before it is re-appended.
// A tenant URL copied from a browser often already ends in "/jobs", and blindly
// appending produced ".../<site>/jobs/jobs", which Workday answers with 404 or
// 405, a footgun that had silently disabled real tenants in this list.
func workdayCXSURL(host, company, sitePath string) string {
	site := strings.Trim(sitePath, "/")
	site = strings.TrimSuffix(site, "/jobs")

	return fmt.Sprintf("https://%s/wday/cxs/%s/%s/jobs", host, company, site)
}

var WorkdayCompanyURLs = []string{
	"https://3m.wd1.myworkdayjobs.com/Search",
	"https://aah.wd5.myworkdayjobs.com/External",
	"https://adobe.wd5.myworkdayjobs.com/external_experienced",
	"https://adventhealth.wd12.myworkdayjobs.com/AH_External_Career_Site",
	"https://adventisthealthcare.wd1.myworkdayjobs.com/AdventistHealthCareCareers",
	"https://aes.wd1.myworkdayjobs.com/AES_US",
	"https://aig.wd1.myworkdayjobs.com/aig",
	"https://allstate.wd5.myworkdayjobs.com/Agent",
	"https://alterra.wd1.myworkdayjobs.com/AlterraMountainCompany",
	"https://americanredcross.wd1.myworkdayjobs.com/American_Red_Cross_Careers",
	"https://amfam.wd1.myworkdayjobs.com/Careers",
	"https://amgen.wd1.myworkdayjobs.com/Careers",
	"https://astrazeneca.wd3.myworkdayjobs.com/Careers",
	"https://att.wd1.myworkdayjobs.com/ATTGeneral",
	"https://austintexas.wd5.myworkdayjobs.com/COA_Careers",
	"https://bah.wd1.myworkdayjobs.com/BAH_Jobs",
	"https://bannerhealth.wd108.myworkdayjobs.com/Careers",
	"https://baxter.wd1.myworkdayjobs.com/baxter",
	"https://bcbskc.wd1.myworkdayjobs.com/BCBS_External_Career_Site",
	"https://bestbuycanada.wd3.myworkdayjobs.com/BestBuyCA_Career",
	"https://bhs.wd1.myworkdayjobs.com/careers",
	"https://bigcommerce.wd12.myworkdayjobs.com/Commerce",
	"https://bitsight.wd1.myworkdayjobs.com/Bitsight",
	"https://bjswholesaleclub.wd1.myworkdayjobs.com/BJsCareers",
	"https://blueorigin.wd5.myworkdayjobs.com/BlueOrigin",
	"https://boeing.wd1.myworkdayjobs.com/EXTERNAL_CAREERS",
	"https://borgwarner.wd5.myworkdayjobs.com/BorgWarner_Careers",
	"https://boseallaboutme.wd503.myworkdayjobs.com/Bose_Careers",
	"https://bozemanhealth.wd1.myworkdayjobs.com/BozemanHealthCareers",
	"https://bristolmyerssquibb.wd5.myworkdayjobs.com/BMS",
	"https://broadcom.wd1.myworkdayjobs.com/External_Career",
	"https://brownhealth.wd12.myworkdayjobs.com/External_Careers",
	"https://cableone.wd1.myworkdayjobs.com/Cable_One_External_Careers",
	"https://caci.wd1.myworkdayjobs.com/External",
	"https://cae.wd3.myworkdayjobs.com/career",
	"https://capitalone.wd12.myworkdayjobs.com/Capital_One",
	"https://carilionclinic.wd12.myworkdayjobs.com/External_Careers",
	"https://cat.wd5.myworkdayjobs.com/CaterpillarCareers",
	"https://ccf.wd1.myworkdayjobs.com/ClevelandClinicCareers",
	"https://chaptershealth.wd5.myworkdayjobs.com/jobs",
	"https://chipotle.wd5.myworkdayjobs.com/ChipotleCareers",
	"https://choicehotels.wd5.myworkdayjobs.com/External",
	"https://choicehotels.wd5.myworkdayjobs.com/HotelExternal",
	"https://chop.wd108.myworkdayjobs.com/CHOPExternalCareers",
	"https://cigna.wd5.myworkdayjobs.com/cignacareers/",
	"https://citi.wd5.myworkdayjobs.com/2",
	"https://citrix.wd1.myworkdayjobs.com/CitrixCareers",
	"https://coke.wd1.myworkdayjobs.com/coca-cola-careers",
	"https://comcast.wd5.myworkdayjobs.com/Comcast_Careers",
	"https://connexuscu.wd1.myworkdayjobs.com/ConnexusCareers",
	"https://cornell.wd1.myworkdayjobs.com/CCECareerPage",
	"https://crowdstrike.wd5.myworkdayjobs.com/crowdstrikecareers",
	"https://cvshealth.wd1.myworkdayjobs.com/CVS_Health_Careers",
	"https://danafarber.wd5.myworkdayjobs.com/dana-farber",
	"https://dell.wd1.myworkdayjobs.com/External",
	"https://denver.wd1.myworkdayjobs.com/CCD-denver-denvergov-CSC_Jobs-Civil_service_jobs-Police_Jobs-Fire_Jobs",
	"https://disney.wd5.myworkdayjobs.com/disneycareer",
	"https://dnb.wd1.myworkdayjobs.com/Careers",
	"https://dukeenergy.wd1.myworkdayjobs.com/search",
	"https://earlywarning.wd5.myworkdayjobs.com/earlywarningcareers",
	"https://echostar.wd5.myworkdayjobs.com/echostar",
	"https://edwards.wd5.myworkdayjobs.com/EdwardsCareers",
	"https://elevancehealth.wd1.myworkdayjobs.com/ANT",
	"https://epicgames.wd5.myworkdayjobs.com/Epic_Games",
	"https://erm.wd3.myworkdayjobs.com/ERM_Careers",
	"https://expedia.wd108.myworkdayjobs.com/search",
	"https://fedex.wd1.myworkdayjobs.com/FXE-EU_External",
	"https://fedex.wd1.myworkdayjobs.com/FXF-MEX-External",
	"https://ferguson.wd1.myworkdayjobs.com/Ferguson_Experienced",
	"https://fico.wd1.myworkdayjobs.com/External",
	"https://fifththird.wd5.myworkdayjobs.com/53careers",
	"https://finning.wd3.myworkdayjobs.com/External",
	"https://flagstar.wd5.myworkdayjobs.com/flagstar",
	"https://foxfactory.wd1.myworkdayjobs.com/FOX",
	"https://gapinc.wd1.myworkdayjobs.com/GAPINC",
	"https://gatesfoundation.wd1.myworkdayjobs.com/Gates",
	"https://generalmotors.wd5.myworkdayjobs.com/Careers_GM",
	"https://georgetown.wd1.myworkdayjobs.com/Georgetown_Admin_Careers",
	"https://ghr.wd1.myworkdayjobs.com/lateral-us",
	"https://gilead.wd1.myworkdayjobs.com/gileadcareers",
	"https://globalhr.wd5.myworkdayjobs.com/REC_RTX_Ext_Gateway",
	"https://godaddy.wd1.myworkdayjobs.com/GoDaddy_careers",
	"https://gohealthuc.wd12.myworkdayjobs.com/External",
	"https://guardianlife.wd5.myworkdayjobs.com/Guardian-Life-Careers",
	"https://hagerty.wd5.myworkdayjobs.com/hagerty",
	"https://hcahealthcare.wd3.myworkdayjobs.com/hcacareers",
	"https://heinz.wd1.myworkdayjobs.com/KraftHeinz_Careers",
	"https://helenoftroy.wd503.myworkdayjobs.com/Main_HoT",
	"https://henryschein.wd1.myworkdayjobs.com/External_Careers",
	"https://hfsc.wd503.myworkdayjobs.com/Careers",
	"https://hhc.wd5.myworkdayjobs.com/HHC",
	"https://homedepot.wd5.myworkdayjobs.com/CareerDepot",
	"https://hshs.wd1.myworkdayjobs.com/hshscareers",
	"https://huntington.wd12.myworkdayjobs.com/HNBcareers",
	"https://ibotta.wd1.myworkdayjobs.com/Ibotta",
	"https://iheartmedia.wd5.myworkdayjobs.com/External_iHM",
	"https://imh.wd108.myworkdayjobs.com/IntermountainCareers",
	"https://jbhunt.wd501.myworkdayjobs.com/Careers",
	"https://jda.wd5.myworkdayjobs.com/JDA_Careers",
	"https://jj.wd5.myworkdayjobs.com/JJ",
	"https://jmh.wd5.myworkdayjobs.com/JohnMuirHealthCareers",
	"https://kansashealthsystem.wd1.myworkdayjobs.com/careers",
	"https://kantar.wd3.myworkdayjobs.com/KANTAR",
	"https://kbr.wd5.myworkdayjobs.com/KBR_Careers",
	"https://keybank.wd5.myworkdayjobs.com/External_Career_Site",
	"https://kohls.wd504.myworkdayjobs.com/kohlscareers",
	"https://kumc.wd5.myworkdayjobs.com/kumc-jobs",
	"https://leidos.wd5.myworkdayjobs.com/External",
	"https://lilly.wd115.myworkdayjobs.com/LLY",
	"https://livenation.wd503.myworkdayjobs.com/LNExternalSite",
	"https://lowes.wd5.myworkdayjobs.com/LWS_External_CS",
	"https://marvell.wd1.myworkdayjobs.com/MarvellCareers",
	"https://massgeneralbrigham.wd1.myworkdayjobs.com/MGBExternal",
	"https://massmutual.wd1.myworkdayjobs.com/MMCareers",
	"https://mastercard.wd1.myworkdayjobs.com/CorporateCareers",
	"https://mckesson.wd3.myworkdayjobs.com/External_Careers",
	"https://medtronic.wd1.myworkdayjobs.com/MedtronicCareers",
	"https://methodisthealthsystem.wd1.myworkdayjobs.com/MHS_Careers",
	"https://mgmresorts.wd5.myworkdayjobs.com/MGMCareers",
	"https://monumenthealth.wd1.myworkdayjobs.com/Goldcareers",
	"https://motorolasolutions.wd5.myworkdayjobs.com/Careers/",
	"https://mpc.wd1.myworkdayjobs.com/MPCCareers",
	"https://ms.wd5.myworkdayjobs.com/External",
	"https://msd.wd5.myworkdayjobs.com/SearchJobs",
	"https://msk.wd108.myworkdayjobs.com/MSKCC_Careers_Primary",
	"https://msmc.wd12.myworkdayjobs.com/msmc_careers",
	"https://mtb.wd5.myworkdayjobs.com/MTB",
	"https://nationwide.wd1.myworkdayjobs.com/Nationwide_Career",
	"https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site",
	"https://nordstrom.wd501.myworkdayjobs.com/nordstrom_careers",
	"https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite",
	"https://nyp.wd1.myworkdayjobs.com/nypcareers",
	"https://nytimes.wd5.myworkdayjobs.com/Tech",
	"https://nyuhs.wd12.myworkdayjobs.com/nyuhscareers1",
	"https://ochsner.wd1.myworkdayjobs.com/Ochsner",
	"https://odfl.wd1.myworkdayjobs.com/ODFL_Careers",
	"https://osu.wd1.myworkdayjobs.com/OSUCareers",
	"https://oxford.wd5.myworkdayjobs.com/TommyBahamaUS",
	"https://panerabread.wd5.myworkdayjobs.com/Panera_Careers",
	"https://petco.wd504.myworkdayjobs.com/External",
	"https://pfizer.wd1.myworkdayjobs.com/PfizerCareers",
	"https://pnc.wd5.myworkdayjobs.com/External",
	"https://premera.wd5.myworkdayjobs.com/premera",
	"https://premierinc.wd1.myworkdayjobs.com/External_Professional",
	"https://promedica.wd12.myworkdayjobs.com/External_Careers",
	"https://proofpoint.wd5.myworkdayjobs.com/ProofpointCareers",
	"https://pru.wd5.myworkdayjobs.com/Careers",
	"https://prudential.wd3.myworkdayjobs.com/prudential",
	"https://pureinsurance.wd5.myworkdayjobs.com/PURE",
	"https://pwc.wd3.myworkdayjobs.com/US_Experienced_Careers",
	"https://qualys.wd5.myworkdayjobs.com/Careers",
	"https://regeneron.wd1.myworkdayjobs.com/Careers",
	"https://regions.wd5.myworkdayjobs.com/Regions_Careers",
	"https://rocket.wd5.myworkdayjobs.com/rocket_careers",
	"https://rollsroyce.wd3.myworkdayjobs.com/professional",
	"https://ryder.wd5.myworkdayjobs.com/RyderCareers",
	"https://sailpoint.wd1.myworkdayjobs.com/SailPoint",
	"https://saintlukes.wd1.myworkdayjobs.com/saintlukeshealthcareers",
	"https://salesforce.wd12.myworkdayjobs.com/External_Career_Site",
	"https://salesforce.wd12.myworkdayjobs.com/Slack",
	"https://salesforce.wd12.myworkdayjobs.com/Tableau",
	"https://sanofi.wd3.myworkdayjobs.com/SanofiCareers",
	"https://santander.wd3.myworkdayjobs.com/SantanderCareers",
	"https://seaworldentertainment.wd1.myworkdayjobs.com/SEA",
	"https://sec.wd3.myworkdayjobs.com/Samsung_Careers",
	"https://servicetitan.wd1.myworkdayjobs.com/ServiceTitan",
	"https://sharp.wd1.myworkdayjobs.com/External",
	"https://snapchat.wd1.myworkdayjobs.com/snap",
	"https://solvenergy.wd1.myworkdayjobs.com/SOLV_External_Career",
	"https://spe.wd1.myworkdayjobs.com/SonyPicturesEntertainment",
	"https://standard.wd1.myworkdayjobs.com/Search",
	"https://statestreet.wd1.myworkdayjobs.com/Global",
	"https://stellantis.wd3.myworkdayjobs.com/External_Career_Site_ID01",
	"https://stoneridge.wd5.myworkdayjobs.com/Careers",
	"https://strayer.wd1.myworkdayjobs.com/SEI",
	"https://stryker.wd1.myworkdayjobs.com/StrykerCareers",
	"https://sunlife.wd3.myworkdayjobs.com/Experienced-Jobs",
	"https://swa.wd1.myworkdayjobs.com/external",
	"https://synchronyfinancial.wd5.myworkdayjobs.com/careers",
	"https://sysco.wd5.myworkdayjobs.com/syscocareers",
	"https://tamus.wd1.myworkdayjobs.com/TAMU_External",
	"https://target.wd5.myworkdayjobs.com/targetcareers",
	"https://td.wd3.myworkdayjobs.com/TD_Bank_Careers",
	"https://tjx.wd1.myworkdayjobs.com/TJX_EXTERNAL",
	"https://tmobile.wd1.myworkdayjobs.com/External",
	"https://topgolf.wd501.myworkdayjobs.com/TopgolfCareers",
	"https://toyota.wd503.myworkdayjobs.com/TMNA",
	"https://transamerica.wd5.myworkdayjobs.com/US",
	"https://travelers.wd5.myworkdayjobs.com/External",
	"https://trinityhealth.wd1.myworkdayjobs.com/Jobs",
	"https://truist.wd1.myworkdayjobs.com/Careers",
	"https://tysonfoods.wd5.myworkdayjobs.com/TSN",
	"https://uaa.wd12.myworkdayjobs.com/EXT",
	"https://uchicago.wd5.myworkdayjobs.com/External",
	"https://uhaul.wd1.myworkdayjobs.com/UhaulJobs",
	"https://ummc.wd5.myworkdayjobs.com/UMCCareers",
	"https://unisys.wd5.myworkdayjobs.com/External",
	"https://unum.wd1.myworkdayjobs.com/External",
	"https://usaa.wd1.myworkdayjobs.com/USAAJOBSWD",
	"https://usacs.wd1.myworkdayjobs.com/usacscareers",
	"https://usbank.wd1.myworkdayjobs.com/US_Bank_Careers",
	"https://usfoods.wd1.myworkdayjobs.com/usfoodscareersExternal",
	"https://uva.wd1.myworkdayjobs.com/UVAJobs",
	"https://valleyhealth.wd12.myworkdayjobs.com/Valley_Health_System_Careers_",
	"https://veritas.wd1.myworkdayjobs.com/careers",
	"https://verizon.wd12.myworkdayjobs.com/verizon-careers",
	"https://visa.wd5.myworkdayjobs.com/Visa",
	"https://vsp.wd1.myworkdayjobs.com/VSPVisionCareers",
	"https://vumc.wd1.myworkdayjobs.com/vumccareers",
	"https://vwr.wd1.myworkdayjobs.com/avantorJobs",
	"https://warnerbros.wd5.myworkdayjobs.com/global",
	"https://wegmans.wd1.myworkdayjobs.com/Wegmans",
	"https://wf.wd1.myworkdayjobs.com/WellsFargoJobs",
	"https://workday.wd5.myworkdayjobs.com/Workday",
	"https://wvumedicine.wd1.myworkdayjobs.com/WVUH",
	"https://wwecorp.wd5.myworkdayjobs.com/wwecorp",
}

// Paging behaviour for a Workday tenant's "cxs" endpoint.
const (
	// workdayPageSize is how many postings each request asks for.
	//
	// Workday's own careers UI asks for 20, which is what this adapter used to
	// do, so a tenant with 3,000 open roles needed 150 strictly sequential round
	// trips. The cxs endpoint is widely used with larger windows and 100 is the
	// value other integrations settle on, so that is what we ask for.
	//
	// This is NOT verified against all ~216 tenants in [WorkdayCompanyURLs];
	// there is no way to probe them from CI, and a page size that some tenant
	// rejects would be a far worse bug than the slow crawl it replaces. So the
	// value is treated as a request rather than a promise, in two ways:
	//
	//   - the pagination stride is taken from how many postings the first page
	//     actually contained, not from what was asked for, so a tenant that
	//     silently clamps to 20 is paged at 20 and nothing is skipped; and
	//   - a first page refused outright is retried once at
	//     [workdayBaselinePageSize] (see workdayShouldRetrySmaller), so a picky
	//     tenant degrades to the old behaviour instead of vanishing from the
	//     crawl.
	workdayPageSize = 100

	// workdayBaselinePageSize is the window Workday's own careers UI uses, and
	// therefore the only one every tenant is known to accept. It is the fallback
	// when a tenant refuses [workdayPageSize].
	workdayBaselinePageSize = 20

	// workdayPageFetchers bounds how many page requests are in flight for one
	// tenant at a time.
	//
	// Workday tenants are host-isolated: httpx.servicePolicyFor deliberately
	// does not group *.myworkdayjobs.com into a shared bucket, so each tenant
	// gets its own limiter key. This bound is therefore per employer, not
	// global.
	//
	// It is deliberately equal to httpx's default per-service limit. The
	// limiter, not this constant, is the politeness ceiling: a larger value here
	// would not send more requests, it would only park more goroutines on the
	// limiter's semaphore.
	workdayPageFetchers = 4

	// workdayMaxPages caps how many pages a single tenant may be asked for.
	//
	// Page offsets are derived from the "total" the tenant itself reports, so a
	// tenant that reports an absurd total, or that ignores the offset parameter
	// and serves page one forever, would otherwise keep the crawl busy until its
	// deadline. At the clamped-to-20 worst case this still allows 20,000
	// postings from one employer, comfortably above the largest tenant observed
	// here, while bounding a misbehaving one at 1,000 requests.
	workdayMaxPages = 1000
)

type workdayInfo struct {
	Total       int `json:"total"`
	JobPostings []struct {
		Title         string   `json:"title"`
		ExternalPath  string   `json:"externalPath"`
		LocationsText string   `json:"locationsText"`
		PostedOn      string   `json:"postedOn"`
		BulletFields  []string `json:"bulletFields"`
	} `json:"jobPostings"`
	Facets []struct {
		FacetParameter string `json:"facetParameter"`
		Descriptor     string `json:"descriptor,omitempty"`
		Values         []struct {
			Descriptor string `json:"descriptor"`
			ID         string `json:"id"`
			Count      int    `json:"count"`
		} `json:"values"`
	} `json:"facets"`
	UserAuthenticated bool `json:"userAuthenticated"`
}

// Workday returns the job postings found at a given Workday URL using the provided HTTP client.
//
// The first page carries the tenant's total, so every remaining page offset is
// known immediately; they are fetched with bounded concurrency
// ([workdayPageFetchers]) and their postings are yielded as they arrive rather
// than in page order. Ordering within one employer was never meaningful, and
// waiting for page N-1 before emitting page N would reintroduce the serial
// round trips this exists to remove.
func Workday(ctx context.Context, httpClient *http.Client, rawURL string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			yield(nil, fmt.Errorf("failed to parse workday URL %q: %w", rawURL, err))
			return
		}

		var (
			host    = parsedURL.Hostname()
			company = workdayCompanyName(rawURL)
			cxsURL  = workdayCXSURL(host, company, parsedURL.Path)
		)

		// Cancelling this context is how a consumer that stops early, or a page
		// that fails, tells the in-flight fetchers to wind down at once. The
		// caller's context is kept separately so "the caller cancelled us" can
		// be told apart from "we cancelled ourselves on the way out".
		parentCtx := ctx

		ctx, cancel := context.WithCancel(parentCtx)
		defer cancel()

		// emit hands one page's postings to the consumer. It reports whether
		// iteration should continue; a false result means either the consumer
		// asked to stop or the caller's context was cancelled, and in the latter
		// case the error has already been yielded.
		emit := func(doc *workdayInfo) bool {
			for _, job := range doc.JobPostings {
				if err := parentCtx.Err(); err != nil {
					yield(nil, err)

					return false
				}

				if !yield(&internal.JobPosting{
					Title:    job.Title,
					URL:      fmt.Sprintf("%s%s", rawURL, job.ExternalPath),
					Location: cmp.Or(job.LocationsText, "unknown"),
					Company:  company,
				}, nil) {
					return false
				}
			}

			return true
		}

		limit := workdayPageSize

		first, err := workdayFetchPage(ctx, httpClient, cxsURL, rawURL, limit, 0)
		if workdayShouldRetrySmaller(limit, first, err) {
			limit = workdayBaselinePageSize
			first, err = workdayFetchPage(ctx, httpClient, cxsURL, rawURL, limit, 0)
		}

		if err != nil {
			yield(nil, err)

			return
		}

		// The stride is what the tenant actually served, not what was asked for,
		// so a tenant that clamps the page size is still paged without gaps.
		total, step := first.Total, len(first.JobPostings)

		// A tenant with nothing open, or one that answers page one with no
		// postings at all, has no second page worth asking for.
		if total == 0 || step == 0 {
			return
		}

		if !emit(first) {
			return
		}

		if total <= step {
			return
		}

		// Bounded by workdayMaxPages, counting the page already fetched, so a
		// tenant reporting an absurd total cannot schedule unbounded work; the
		// capacity is bounded for the same reason.
		offsets := make([]int, 0, min((total-1)/step, workdayMaxPages-1))
		for offset := step; offset < total && len(offsets) < workdayMaxPages-1; offset += step {
			offsets = append(offsets, offset)
		}

		type pageResult struct {
			doc *workdayInfo
			err error
		}

		var (
			results   = make(chan pageResult)
			sem       = make(chan struct{}, workdayPageFetchers)
			exhausted = make(chan struct{})
			exhaust   sync.Once
			wg        sync.WaitGroup
		)

		// stopScheduling is called when a page comes back with no postings at
		// all, which means the tenant's reported total overshot what it will
		// serve; the offsets past that point would only fetch more empty pages.
		//
		// A short but non-empty page is deliberately not treated this way. It is
		// indistinguishable from a tenant hiccuping mid-crawl, and acting on it
		// would silently truncate an employer, which is precisely the class of
		// bug this code exists to fix. Offsets already stop at the reported
		// total, so a genuine short tail costs nothing.
		stopScheduling := func() { exhaust.Do(func() { close(exhausted) }) }

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
					// prefetching runs at most workdayPageFetchers pages ahead of
					// the consumer instead of buffering a whole tenant in memory.
					defer func() { <-sem }()

					doc, err := workdayFetchPage(ctx, httpClient, cxsURL, rawURL, limit, offset)

					select {
					case results <- pageResult{doc: doc, err: err}:
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

			if len(result.doc.JobPostings) == 0 {
				stopScheduling()

				continue
			}

			if !emit(result.doc) {
				stop()

				return
			}
		}

		// A tenant cut short by the caller's cancellation returned partial
		// results. Say so, rather than let a truncated employer look complete.
		if err := parentCtx.Err(); err != nil {
			yield(nil, err)
		}
	}
}

// workdayStatusError reports a non-200 response from a tenant's cxs endpoint.
//
// The status code is retained, rather than only being formatted into the
// message, so the first-page fetch can tell "this tenant refuses the page size
// we asked for" (400/422) apart from "this tenant is gone" (404) and retry only
// the former. The message wording is unchanged from when this was an inline
// fmt.Errorf, so logs and health output do not shift.
type workdayStatusError struct {
	rawURL string
	status string
	code   int
}

// Error implements the error interface.
func (e *workdayStatusError) Error() string {
	return fmt.Sprintf("unexpected status code from workday URL %q: %s", e.rawURL, e.status)
}

// workdayShouldRetrySmaller reports whether a first-page result looks like the
// tenant rejecting [workdayPageSize] rather than the tenant being broken.
//
// Two response shapes are attributable to an unwelcome page size: an outright
// 400 or 422, and a 200 that claims postings exist but carries none. Anything
// else, a 404, a 5xx, a transport error, a decode failure, is reported as-is;
// re-asking with a different page size would only double the load on a tenant
// that is already failing.
func workdayShouldRetrySmaller(limit int, doc *workdayInfo, err error) bool {
	if limit <= workdayBaselinePageSize {
		return false
	}

	var statusErr *workdayStatusError
	if errors.As(err, &statusErr) {
		return statusErr.code == http.StatusBadRequest || statusErr.code == http.StatusUnprocessableEntity
	}

	return err == nil && doc != nil && doc.Total > 0 && len(doc.JobPostings) == 0
}

// workdayFetchPage fetches and decodes a single page of a tenant's results.
//
// The response body is closed on every exit path. This used to be a
// `defer resp.Body.Close()` sitting inside the pagination loop, so every page's
// body stayed open until the whole source function returned. httpx hands back
// bodies wrapped in a releaseOnClose that frees the per-service concurrency slot
// only when the body is closed, and the default limit is 4: every Workday tenant
// therefore fetched exactly four pages (80 postings), then blocked on the
// semaphore until the client's two-minute timeout fired. Measured across ~216
// tenants that silently truncated every Workday employer at 80 postings and
// burned two minutes of a worker slot per tenant doing nothing. The health
// command never caught it because its check caps at 100 postings.
func workdayFetchPage(ctx context.Context, httpClient *http.Client, cxsURL, rawURL string, limit, offset int) (*workdayInfo, error) {
	payload := fmt.Sprintf(`{"appliedFacets":{},"limit":%d,"offset":%d,"searchText":""}`, limit, offset)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cxsURL, strings.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request for workday URL %q: %w", rawURL, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to workday URL %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Bounded drain so the connection returns to the pool rather than being
		// torn down mid-response; a tenant's error page can be a full HTML site.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

		return nil, &workdayStatusError{rawURL: rawURL, status: resp.Status, code: resp.StatusCode}
	}

	var doc workdayInfo
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode response from workday URL %q at offset %d: %w", rawURL, offset, err)
	}

	return &doc, nil
}
