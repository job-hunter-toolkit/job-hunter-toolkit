package services

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
			offset  = 0
			limit   = 20
		)

		for {
			payload := fmt.Sprintf(`{"appliedFacets":{},"limit":%d,"offset":%d,"searchText":""}`, limit, offset)

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, cxsURL, strings.NewReader(payload))
			if err != nil {
				yield(nil, fmt.Errorf("failed to create request for workday URL %q: %w", rawURL, err))
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")

			resp, err := httpClient.Do(req)
			if err != nil {
				yield(nil, fmt.Errorf("failed to make request to workday URL %q: %w", rawURL, err))
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				yield(nil, fmt.Errorf("unexpected status code from workday URL %q: %s", rawURL, resp.Status))
				return
			}

			var doc workdayInfo
			if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
				yield(nil, fmt.Errorf("failed to decode response from workday URL %q: %w", rawURL, err))
				return
			}

			if doc.Total == 0 || len(doc.JobPostings) == 0 {
				return
			}

			for _, job := range doc.JobPostings {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				if !yield(&internal.JobPosting{
					Title:    job.Title,
					URL:      fmt.Sprintf("%s%s", rawURL, job.ExternalPath),
					Location: cmp.Or(job.LocationsText, "unknown"),
					Company:  company,
				}, nil) {
					return
				}
			}

			if doc.Total <= offset+len(doc.JobPostings) {
				return
			}

			offset += limit
		}
	}
}
