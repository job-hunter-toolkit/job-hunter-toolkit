package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// breezyPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const breezyPlatform = "breezy"

func init() {
	registerBuiltin(breezyPlatform, multiJobsFunc(Breezy, BreezyCompanies))
}

// BreezyCompanies holds the Breezy HR career sites this project crawls, one
// tenant subdomain per entry: "matroid" is https://matroid.breezy.hr.
//
// Breezy is the cheapest lane measured in this project so far. One keyless GET
// returns a tenant's entire open-req list — there is no pagination at all, no
// page parameter, and no detail fetch, because the only field the list omits is
// the description and [internal.JobPosting] has nowhere to put one. The list
// carries title, public URL, posting id, department, employment type, a
// structured location with a remote flag, a published date, and for the tenants
// that opt in an employer-published pay string.
//
// # This list is measured, not staged
//
// Every entry below answered a live probe on 2026-07-28. The full 1,948-slug
// candidate file at testdata/candidates/breezy_slugs.txt was probed at
// https://<slug>.breezy.hr/json under the shared limiter key *.breezy.hr needs,
// four requests in flight at a time. See that file's header for the counts and
// the promotion rule; the short version is that a slug is registered here when
// it answered HTTP 200 with a JSON array carrying at least one position that had
// both a "name" and an https "url", which is exactly what this adapter needs to
// emit a posting.
//
// Verification is unusually clean on this platform, and worth recording because
// docs/adding-a-source.md warns that several platforms serve a 200 landing page
// for unknown tenants. Breezy does not: a slug with no board answers HTTP 302 to
// https://breezy.hr/, and the marketing site behind that redirect answers 403 to
// this project's User-Agent. So a dead tenant can never be mistaken for an empty
// one here, and fetchJSON rejects both without any content check.
var BreezyCompanies = []string{
	"1-grid",
	"10-4-truck-recruiting",
	"1001",
	"1234s",
	"1440-foods-manufacturing",
	"20four7va",
	"25madison-llc",
	"2ulaundry",
	"360-apartment-renovations",
	"3brasseurs",
	"3ex",
	"4th-day-trucking",
	"6037",
	"75f",
	"75f-apac",
	"7am-enfant-inc",
	"a-core-concrete-specialists",
	"a-hiringgroup",
	"a2h",
	"abc-imaging",
	"ablefy",
	"absolute-pets",
	"abstractgroup",
	"accentit",
	"accrete-ai",
	"aceandcompany",
	"acme",
	"actimage",
	"activate-group-limited",
	"active-dental-management",
	"acute-nursing-care",
	"adah-international-llc",
	"adapt",
	"adnet-accountnet-inc",
	"adobe",
	"adoc-tm",
	"adquick",
	"advance-financial",
	"advanced-crypto-services-inc",
	"advanced-dri",
	"advanced-nursing-concepts-llc",
	"advantage-design-group",
	"advantia-health",
	"aeroseal",
	"af",
	"aflac-central-southern-illinois",
	"agbo",
	"aimpoint-digital",
	"aira",
	"akt",
	"alabama-oncology",
	"alasco",
	"alaya",
	"alchemy-financial-group",
	"alconost-inc",
	"aleph-ventures",
	"aligned-geriatrics",
	"aligned-modern-health",
	"all-financial-freedom",
	"allcares",
	"alliance",
	"allies4outcomes",
	"alltrade-industrial-contractors",
	"alma-del-mar-charter-school",
	"almentor",
	"alpaca-racing",
	"alphanumeric-systems",
	"alpin-gmbh",
	"alt-legal",
	"altanova",
	"altitude-media-group",
	"altodia",
	"amazingtalker",
	"american-academy",
	"american-air-conditioning-heating",
	"american-antiquarian-society",
	"american-civil-liberties-union-wisconsin",
	"american-coatings-association",
	"american-council-of-engineering-companies",
	"american-logistics-authority",
	"ampersand-rwanda-ltd",
	"anchor-conzult",
	"anchour",
	"ancient-crunch",
	"andonovo",
	"andworx",
	"angiosafe",
	"anka",
	"answeraide",
	"anthos-home",
	"antidote",
	"ao-national-globe-life",
	"apex-tk-llc",
	"apm-music",
	"apollo-after-school",
	"appleseed",
	"applied-imagination",
	"apriorit",
	"aqua-blue-pools",
	"aquino-capital-group-llc-empowered-by-nexa-mortgage-llc",
	"arch-systems",
	"archangelautonomy",
	"archangellightworks",
	"archsoftware",
	"aristeo",
	"aroma360",
	"arpio",
	"arrived",
	"artemis-connection",
	"arthrex-indianapolis",
	"artidis-ag",
	"artifex-interior-systems-limited",
	"artis-llc",
	"asb-freight-co",
	"asc",
	"ascent-robotics",
	"ascento-ag",
	"ash-wellness-inc",
	"asian-law-caucus",
	"asian-pacific-islander-legal-outreach",
	"asian-pacific-islander-political-alliance",
	"askable",
	"aspinal-of-london",
	"aspria",
	"assessment-intervention-management",
	"assignar",
	"astanza-laser",
	"astro-shapes-llc",
	"at-work-cincy",
	"atbs",
	"athletes-first",
	"atlas-technica",
	"atlasprimary",
	"atp-flight-school",
	"ats-automation",
	"auric",
	"aurora-consulting",
	"aurumverse",
	"authentic",
	"avanceon",
	"aventus-llc",
	"avi-health-community-services-9eb7555be744",
	"aviation-technology-associates-llc",
	"avidyne",
	"aviron",
	"awardspring",
	"awarri",
	"axcend-chromatography",
	"backyard-discovery",
	"bad-marketing",
	"baisch-engineering",
	"baked-bros",
	"bald-head-island-club",
	"barharborinn",
	"barloworldequipment",
	"barn2door-inc",
	"barnhart-crane-rigging",
	"barupon-llc",
	"bassgordon",
	"battle-house-laser-tag",
	"be-caring-ltd",
	"beacon-inspection",
	"beagle-services",
	"bear-robotics",
	"beech-valley-solutions",
	"beefree",
	"beets-catering",
	"beex",
	"behavior-treatment-analysis",
	"behavioral-health-field-inc",
	"beli",
	"bellesa",
	"benchmark-space-systems-inc",
	"benetnaschllc",
	"benzinga",
	"beta-bionics-inc",
	"betclic-group",
	"betterme",
	"bettersource",
	"bgco",
	"bibrave",
	"bidvestbank",
	"big-brothers-big-sisters-of-america",
	"big-fat-smile-group-ltd",
	"birthday-capsule",
	"bitbean",
	"bitdeer",
	"black-airplane",
	"black-diamond-agency",
	"black-ink-business-services",
	"black-mcdonald-limited",
	"blackbirds",
	"blanton-peale-institute",
	"blaze-media",
	"blenderbox",
	"blue-projects",
	"blue-sky-hospitality-solutions",
	"blue-sky-pest-control",
	"bluegrass-technologies-pty-ltd",
	"bluetree-group",
	"bmoc-group",
	"bmwc-constructors-inc",
	"bobandronna",
	"boldare",
	"boll-branch",
	"bond",
	"boss-tech",
	"bosun-25f9d5ec70da",
	"bota-systems-ag",
	"bounce-therapy",
	"boxbar-tech",
	"bradshaw-home",
	"brainbox-automations",
	"brandmomentum",
	"brandttractor",
	"breakthrough-inventions",
	"brella",
	"brightside-collective",
	"brilliant",
	"bristow-and-sutor-group",
	"brookhill",
	"buddhist-compassion-relief-tzu-chi-foundation-singapore",
	"bulla",
	"bullfinch",
	"bullseye-strategy",
	"busha",
	"business-staffing-of-america-inc",
	"business-system-solutions",
	"bvn",
	"c2-gps-dba-workforce-solutions-of-the-coastal-bend",
	"c3industries",
	"ca9-employment",
	"cal-com",
	"caliber-fitness",
	"calybre",
	"camber-creative",
	"cambridge-dental",
	"canadian-base-operators",
	"capricorn",
	"captainbook",
	"carbonhound-inc",
	"cardahealth",
	"cardiac-study-center",
	"cardinal-technology-systems-corp",
	"cardioquip",
	"care-of-chan",
	"careers-hankooktire",
	"caribou",
	"cariina",
	"caring",
	"carmen-schools-of-science-technology",
	"carnegie-robotics",
	"casa-speech-and-development",
	"cdh",
	"cdpdoctor",
	"cefaly-technology",
	"census",
	"center-for-care-innovations",
	"center-for-responsive-schools",
	"centerstateceo",
	"central-illinois-pride",
	"centrl",
	"century-21-judge-fite",
	"century-consulting-services",
	"ceresti-health",
	"cesar",
	"chareth-consulting",
	"chat-assassins",
	"chesapeake-specialty-care",
	"chess-wizards",
	"chief-isaac-group-of-companies",
	"children-s-choice-learning-center",
	"children-s-dental-funzone",
	"children-s-healing-center",
	"chirohd",
	"church-of-the-city",
	"church-of-the-highlands",
	"circle-medical",
	"city-of-baltimore-mayor-s-office-of-employment-development",
	"city-of-gulfport",
	"city-report-inc",
	"citylodgehotels",
	"ckh-group",
	"claros-technologies",
	"clean-air-task-force",
	"cleantech-service-group-ltd",
	"clear-one-advantange",
	"clearly",
	"clever-real-estate",
	"cleverbee-academy-llc",
	"cleverclicks",
	"clicklearning",
	"climate-defiance",
	"climavision",
	"cloudcommerce",
	"clove-twine",
	"cloverleaf-bio",
	"club-scikidz-md",
	"cluster-1-sa",
	"coc-consulting",
	"codebase",
	"coeur-d-alene-resort",
	"cogentlabs",
	"cognizo",
	"coherence-os-inc",
	"collectiv",
	"colonial-surety-company",
	"columbia-greene-community-college",
	"combient",
	"cometeer",
	"commercial-cleaning-services",
	"committee-for-children",
	"commonhouse",
	"community-dental-partners",
	"community-health-net",
	"community-youth-network",
	"compass-datacenters",
	"compass-family-services",
	"compose-ly",
	"comprehensive-rehab-consultants",
	"concurrent-technologies-corporation",
	"confidence-health-resources",
	"connect2bpo-sas",
	"connectu-staffing-solutions",
	"continued",
	"contrarian-thinking",
	"contravent-48f199342f2b",
	"converge-cybersecurity-insurance",
	"converge-medical-technology",
	"converge-strategies",
	"corafone-inc",
	"core-mkt",
	"core-ohio-inc",
	"cornbread-hemp",
	"corporate-traffic-logistics",
	"countablelabs",
	"covance-latinoam-rica",
	"cove",
	"cox-pllc",
	"cq-fluency",
	"crafted-staff",
	"crank",
	"creative-living",
	"creative-real-estate-pros",
	"critical-software",
	"crodu",
	"cromwell-architects-engineers-inc-e1e86af4125f",
	"cronoseuropa",
	"cross-screen-media",
	"crossroads-talent-solutions-llc",
	"crowd-cow",
	"cse-global",
	"csi-global",
	"ct-united-fc",
	"ctrack",
	"cumula3",
	"curbee",
	"customfoods",
	"custommade",
	"cyber-advisors",
	"cyberlogic",
	"cybervance",
	"cyborgmobile",
	"cycan-industries",
	"cyos-solutions",
	"cytracom",
	"cz-logistics",
	"daintta-ltd",
	"dalal-mehta-law-llc",
	"dalcom-llc",
	"dalespharmacy",
	"dark-horse-technologies-llc",
	"darkhorse-tech",
	"darwins",
	"datamaxis",
	"davidson-logistics-inc",
	"ddrb",
	"deanhouston",
	"deca-games",
	"deep-end-talent-strategies",
	"deepar",
	"defendify",
	"delicious-monster",
	"delivery-solutions",
	"democracy-summer",
	"dental-depot",
	"dental-health-associates",
	"dermafix-spa",
	"desfo",
	"dev-partners-philippines",
	"developex",
	"device-insight",
	"dewpoint",
	"dex-labs",
	"digikai-marketing",
	"digital-air-strike",
	"direct-to-locums",
	"disperse",
	"disruptive-advertising",
	"divadance",
	"dms-international",
	"dogdaycare",
	"dolly-s-life-of-many-colors-museum",
	"done-plumbing-and-heating",
	"doneverse",
	"doodle-labs-llc",
	"doorstead",
	"dovelewis-veterinary-emergency-and-specialty-hospital",
	"downtown-denver-partnership",
	"doylestown-dental-cosmetic-center",
	"dp-logistics",
	"dreamforge-games-corporation",
	"dressamed",
	"drhouse-inc",
	"drivepersonnel",
	"driver-ai-inc",
	"drop-genie",
	"dropit",
	"drumanilra",
	"dtcp",
	"dubizzlelabs",
	"duolingo",
	"dyflex",
	"e-e-tech",
	"e2e",
	"ea-gibson",
	"eagle-technologies-llc",
	"eai",
	"early-learning-company",
	"easyhealth",
	"easypayfinance",
	"ecic",
	"ecoceres",
	"ecostage",
	"edfpowersolutions",
	"edops",
	"edrolo",
	"eduevolve",
	"ehvert-engineering-inc",
	"eigenblue",
	"eigroup",
	"electra-vehicles-inc",
	"elevate-staffing",
	"elevation-church",
	"elite-amenity-management",
	"elite-private-chefs",
	"embraer",
	"empowerrd",
	"empromptu",
	"empyrean-hospice",
	"enapter-gmbh",
	"encon-equipment",
	"end",
	"endossed",
	"engage-squared",
	"english-musicals-korea",
	"ensemble-performing-arts",
	"entermotion",
	"enterprise-ventures-corporation",
	"erbis",
	"erdman-anthony",
	"etiqa-srl",
	"etoile-academy-charter-school",
	"eurekarobotics",
	"european-digital-finance-association",
	"european-pubs",
	"everlight-solar",
	"everstar",
	"evexias-health-solutions",
	"evidencecare",
	"excellent-recruitment",
	"executive-insight",
	"exf",
	"exit-factor",
	"exit-factor-of-grand-rapids-and-lansing",
	"exo-arnvind",
	"eyesoneyecare",
	"faac-group",
	"fabio-viviani-hospitality-group",
	"factal",
	"fair-harbor",
	"fair-trade-outsourcing",
	"fairsquare",
	"family-resource-home-care",
	"fastenal",
	"fatturaelettronicapa",
	"finstrat-management",
	"fireclay-partners",
	"first-leap",
	"firstier-banks",
	"fix-com",
	"flanco",
	"flat-bridge",
	"fleetster",
	"float-health",
	"florida-energy-advisors",
	"flossy",
	"flowplay-llc",
	"fluxit",
	"focus-group-panel",
	"foodcraft-catering-events",
	"forge-nano",
	"forrest-technical-coatings",
	"fors-marsh-group",
	"foundation-for-advanced-education-in-the-sciences-inc",
	"foundation-for-jewish-camp",
	"foundationhealth",
	"founder-s-cpa",
	"founders-workshop",
	"framework",
	"freeeup",
	"fresh-ventures-studio",
	"fs-isac",
	"fsma",
	"fuelerate",
	"fundview",
	"fx-digital",
	"gaggle",
	"galveston-county-health-district",
	"gamebreaking-studios-inc",
	"gamegrid",
	"gamurs",
	"gaskinslecraw",
	"gastro-health",
	"gateway-market",
	"gaycenter",
	"gecko",
	"gen-tech",
	"genestack-ltd",
	"george-f-young",
	"george-s-tradition",
	"german-american-chambers-of-commerce",
	"ggwp",
	"ghc",
	"giftify",
	"givzey",
	"glance",
	"glenholme-healthcare",
	"global-infotek-inc",
	"glocalzone",
	"gno-partners",
	"go-hr",
	"gold-care-homes",
	"golden-care-of-northeast-pa",
	"goodcents",
	"goodplanet-vzw",
	"goodwipes",
	"gozem",
	"gozio",
	"gp-enterprise-solutions",
	"gpfs",
	"gps-group-peer-support-llc",
	"grace-health",
	"graphaware",
	"gray-capital-llc",
	"green-pest-management",
	"greensdrugmart",
	"grid-aero",
	"grid-united",
	"gritmind",
	"groundwork-coffee",
	"groupsolver",
	"growmore-marketing",
	"growth-marketing-pro-llc",
	"growthub-agency",
	"gsmgroup-africa",
	"guild-care-group-llc",
	"guitar-shed",
	"h-s-loss-control-inspections-in",
	"hahow",
	"hall-ambulance",
	"hall-and-hall",
	"hall-cpa-llc",
	"hamilton-recruitment",
	"handsome-brook-farms",
	"happy-dad",
	"happymed",
	"hart--hickman",
	"hassle-free-home-services",
	"hatch-innovations-canada",
	"hctec",
	"headx",
	"healthy-gamer",
	"heartland-womens-health",
	"hedgehog-technologies-inc",
	"helping-hand-nurse-llc",
	"helpt",
	"helpware-inc",
	"heo",
	"heritage-roofing-and-construction",
	"herocoders",
	"herotel-5f74feffb7e1",
	"hgcareers",
	"hidden-talent",
	"high-performance-real-estate-advisors",
	"highfashionhome",
	"highgrowth-io",
	"highkey",
	"highlights-healthcare",
	"highprofilecannabis",
	"hilton-garden-inn-milwaukee-airport",
	"hioperator",
	"hip",
	"home-genius-exteriors",
	"homebuys-inc",
	"honolulu-authority-for-rapid-transportation",
	"hope-church",
	"hope-city",
	"hopital-fribourgeois",
	"hospitality-health-er",
	"hotelrunner",
	"hotman-group-llc",
	"how-to-manage-a-small-law-firm",
	"hr-performance--results",
	"hstk",
	"http-bienvillelumber-com",
	"http-www-westsidesocialpella-com",
	"hudhud",
	"human-capital-recruiting",
	"human-kinetics",
	"hungerrush",
	"hunt-forest-products",
	"hygienistcareers",
	"i-tech",
	"i5invest",
	"ic-automation",
	"icstars",
	"idea-peddler",
	"ifixit",
	"iix-global",
	"il-gabbiano-societa-cooperativa-sociale-onlus",
	"ilo-group",
	"immanuel-anglican-church",
	"immerse-vr",
	"impact-chw",
	"impireum-managed-services",
	"inallmedia-llc",
	"incubeta-global",
	"independence-excavating",
	"independent-food-company",
	"indiana-county-conservation-district",
	"indiana-repertory-theatre",
	"indinterns",
	"infinit-us",
	"infinite",
	"inflection-point-learning",
	"infocentric",
	"informaticon",
	"infra-pipe-solutions",
	"ingenius-prep",
	"innovativ-pharma-inc",
	"ino-ca",
	"inorg-global",
	"insentra",
	"insightful",
	"insource-services-group",
	"inspired-testing",
	"inspiring-lives-today",
	"insultech",
	"integral-uk",
	"integritas",
	"intelassist",
	"inteldot",
	"intellirad-imaging",
	"international-automotive-components",
	"intiveo",
	"isaac-health",
	"ishir",
	"islide",
	"ismira",
	"itasca-consulting-group",
	"iterate",
	"j-higgins-counseling-lcsw-p-c",
	"j-rose-enterprises-llc",
	"jamaica-ssg",
	"jarvis-ml",
	"jay-analytix",
	"jdee-transport-services",
	"jha-companies",
	"jibble-group",
	"jkz-llp",
	"jlm-hr-consulting",
	"jmi-reports",
	"jobs",
	"jobs-the-long-drink-company",
	"john-flatley-company",
	"johnson-security-bureau-inc",
	"jonathan-s-landing-golf-club",
	"joom-group",
	"jumbo-consulting-group-a-s",
	"jumpseat",
	"justfix",
	"jway-group",
	"k2-electric",
	"kaipod-learning",
	"kaniksu-community-health",
	"kare",
	"karen-clark-company",
	"kastech-canada-inc",
	"keeper-solutions",
	"kegmil",
	"keiki",
	"kellymossinc",
	"kenect",
	"kennebecasis-drugs",
	"keshet-inc",
	"kickstart-accounting-inc",
	"kiddiekredit",
	"kiira-health",
	"kiki-club",
	"kimmel-associates",
	"kindgeeks",
	"kindred-bravely",
	"kitchenmate",
	"kmg-prestige",
	"knowlej",
	"knutson-construction",
	"kompasbank",
	"kowalski-companies-inc",
	"kreative-technologies-llc",
	"kredivo-group",
	"kwil",
	"lab37",
	"labor-mobility-partnerships",
	"ladder",
	"ladder-health",
	"landmark-hospitality",
	"lastro",
	"latica-ai",
	"laulau",
	"leadfuze",
	"leading-financial-advisory-firm",
	"leadzloco",
	"leap-digital-marketing",
	"learn-corporation",
	"learning-people",
	"legna-software",
	"lemonlight",
	"leobit",
	"lestars-management-consultancy-l-l-c",
	"levy-electric-inc",
	"lexlegis",
	"lindt-sprunglicanada",
	"linear-agency",
	"lineleader",
	"lionsville",
	"lite-e-commerce",
	"litostroj-power",
	"location3-media",
	"logan-a-c-heat-services-b29ac6f9712d",
	"loopring",
	"lorain-county-commissioners",
	"love-corn",
	"love-serve-remember",
	"lovevery",
	"ltg",
	"lucky-logic",
	"lufco",
	"lulalend",
	"lumerate",
	"lumio-dental",
	"lumio-dental-practice-locations",
	"lunajets-sa",
	"lunarresources",
	"lyxbil-technologies",
	"m2w-inc-experiential-staffing",
	"machinio",
	"macro-connect",
	"madison-core-laboratories",
	"madison-medical-affiliates",
	"madventure",
	"mae-health",
	"magellan-ai",
	"magnolia-care-services-inc",
	"maidthis",
	"mainstreaming-spa",
	"makerkids",
	"maleda-tech",
	"manabie",
	"mantic",
	"manyone",
	"manypixels",
	"map-international",
	"marex",
	"marketview-education-technology",
	"marshall-dennehey",
	"martin-systems",
	"massfire-media-llc",
	"masters-insurance",
	"masterworks",
	"matrix-design-group",
	"matrix-technologies-inc",
	"matroid",
	"matter-family-office",
	"maverick-collective",
	"mawari-technologies-inc",
	"mccarthy-uniforms",
	"mch-careers",
	"mclaurin-law",
	"mcsteen-land-surveyors",
	"md-exam-inc",
	"meals-on-wheels-central-texas-in-home-care",
	"media-cause",
	"medschoolcoach",
	"megasol-energie-ag",
	"mello",
	"meltzer-hellrung-llc",
	"mendota-insurance-company",
	"merchandising-consultants-associates",
	"mercury-radio-arts",
	"meridian-university",
	"merit-manufacturing",
	"meron-financial-agency",
	"methode",
	"metrum-research-group",
	"michigan-community-resources",
	"microhabitat",
	"mile-high-labs",
	"millbrook-school",
	"miller-musmar",
	"mind-computing",
	"mindoula-health-inc",
	"mission-viejo-consulting-group",
	"missionovo",
	"mk-think",
	"mkec-engineering-inc",
	"mktg",
	"mojorecruit",
	"monocle",
	"montel-intergalactic",
	"mood",
	"morgan-murphy-media",
	"morgan-s-wonderland-camp",
	"moser-consulting",
	"mots-cles",
	"mott-haven-academy-charter-school",
	"movandi",
	"movimentum",
	"movista-inc",
	"mtheory",
	"mtm-llc",
	"mukuru",
	"murphy-s-pharmacies",
	"mussett-nicholas-associates",
	"my-burger",
	"myer-companies-llc",
	"myside",
	"mytonomy-inc",
	"national-assemblers-inc",
	"national-math-stars",
	"national-mortgage-field-services",
	"native-network-inc",
	"natures-herbs-and-wellness",
	"navaide",
	"nebraska-crossing",
	"needle",
	"nepf-llc",
	"net2phone",
	"netrix-global",
	"netsea-technologies",
	"netsync-network-solutions",
	"neurelo",
	"new-era-adr",
	"new-horizon-counseling-center",
	"new-incentives",
	"new-york-holdings",
	"new-york-life",
	"newground",
	"newworld-inc",
	"nexo",
	"nexplay-consulting-inc",
	"next-generation-inc",
	"nexthire",
	"nexu",
	"nexus-group",
	"nexxis-solutions",
	"ngdesignwave",
	"niceplay-games",
	"nimble-group",
	"nine-feet-tall",
	"ninjaholdings",
	"noaca",
	"nomics",
	"norbert-health",
	"nordcloud-career",
	"norman-international-inc",
	"norred-fire",
	"north-river-home-care",
	"north-wind-group",
	"notably",
	"novaces-llc",
	"novipro",
	"novo-properties",
	"npb-companies",
	"ns-staff-agency-ltd",
	"ntc",
	"nucleusteq",
	"nursedash",
	"nursing-float-health",
	"nurturecare",
	"nuserve",
	"nuview",
	"nyic",
	"nysonian",
	"o-keefe-media-group",
	"o-m-plumbing-services-inc",
	"o-mally-management-group",
	"o8",
	"observador",
	"office-puzzle",
	"officetotal-food-brands-lda",
	"omce",
	"one-step",
	"onebridge",
	"onedome",
	"onehope",
	"oneport-365",
	"ontrac-solutions-llc",
	"open-door-legal",
	"opendrives-llc",
	"openmindhealth",
	"openresearch-gmbh",
	"opterus",
	"optieum",
	"optimindhealth",
	"orbital-paradigm",
	"oreganos",
	"organization-for-research-and-learning",
	"osec",
	"otter-pr",
	"otto-engineering-inc",
	"ourassistants",
	"out-of-the-box",
	"p3-usa",
	"pagefreezer-software-inc",
	"pals-home-health",
	"park-avenue-center",
	"park-west-gallery",
	"partner-retail-services",
	"pasa-sustainable-agriculture",
	"passion",
	"passiondc",
	"passtimegps",
	"pastel",
	"pave-academy-charter-school",
	"paynovate",
	"pdmi",
	"pearl-abyss-america",
	"pediatric-dentist",
	"peninsula-canada",
	"people-solutions-center",
	"peoplefluent",
	"peptone",
	"percent-technologies",
	"personified-tech",
	"pharma-medica-research-inc",
	"philadelphia-green-capital-corp",
	"physicians-insurance-a-mutual-company",
	"pickles",
	"pillar-properties",
	"pinelake-church",
	"pinnacle-cares",
	"pixery",
	"pixieset",
	"pizza-luce",
	"plan-left-llc",
	"planted-solar-inc",
	"plastics-family-americas",
	"plat4mation",
	"platinum-fundraising",
	"playstudios-asia",
	"pleasant-valley-corporation",
	"plexure",
	"pogo",
	"pompa-program",
	"ponessa-behavioral-health",
	"pop-mart-americas-inc",
	"poseidon",
	"positive-impact-dental-alliance",
	"pr-volt",
	"prageru",
	"precision-power",
	"precision-strategies",
	"precision-well-servicing",
	"prelude-prep",
	"premier-fitness-service",
	"premier-health-group",
	"prep-academy-tutors",
	"prestige-capital-group",
	"pretty-little-adventures",
	"pri",
	"prica-global-enterprises-inc",
	"principles-recovery",
	"printfly",
	"privacyworks-consulting-inc",
	"prodeo-academy",
	"production-club",
	"proems",
	"progesys-inc",
	"project-26-pennsylvania",
	"prolift-rigging",
	"proofpilot-inc",
	"propelamerica",
	"proper-expression",
	"property-leads",
	"property-meld",
	"prospa",
	"prudent-engineering",
	"psc",
	"pse-healthy-energy",
	"psm",
	"psyphycare-7d99d547f19b",
	"pt-git-gow-ayo",
	"pulse-id",
	"purple-unicorn",
	"puzzle-cats",
	"pyrovio",
	"q-block-computing",
	"qualfio",
	"quality-enterprises-usa-inc",
	"quantios",
	"quatrain-creative",
	"quinn-co-of-ny-ltd",
	"quintessa-marketing",
	"quintessential-health",
	"r3-roofing",
	"radiant-church",
	"radiant-plumbing-and-air-conditioning",
	"raftelis",
	"raisingthevillage",
	"raldex",
	"ramptalentjobs",
	"rapid-delivery-solutions",
	"rebound-technologies",
	"recess",
	"reconstruct-inc",
	"recruiting",
	"red-antler",
	"red-cup-it-inc",
	"red-rabbit",
	"redial-bpo",
	"reel-father-s-rights",
	"refibuy-inc",
	"reiss",
	"reliant-collegiate-program",
	"reliantinternships",
	"remerge",
	"remote-craft",
	"renewable-properties",
	"renewance-inc",
	"renoairport",
	"rentengine",
	"rentmonster",
	"repair-the-world",
	"resilient-minds-on-the-front-lines",
	"resultslab",
	"resultstack",
	"resupply",
	"revamp-engineering-inc",
	"revel-cpa",
	"reveleer",
	"revgen",
	"revolution-recruiting",
	"revops-inc",
	"rh2-engineering-inc",
	"rhynocare",
	"ridepanda",
	"rightmove-health",
	"ritholtz-wealth-management",
	"rivermead",
	"rk-brands-ltd",
	"rlay",
	"roc-ventures",
	"rockcruit",
	"rombo-ai",
	"rootstock-software",
	"rose-roofing-restoration",
	"rotating-equipment-specialists",
	"rs-breakers-and-controls-inc-cb1b471cbe8e",
	"rubicon-group",
	"rumble-boxing",
	"ruvixx",
	"ryther-child-center",
	"s-a-group",
	"s-b-technical-products",
	"sa-global",
	"sabanto",
	"safarimicro",
	"safe-passage-project-corporation",
	"sage-haus",
	"sales-excellence-institute",
	"salesdraft-recruiting",
	"salesflow",
	"salesrabbit",
	"salmonjobs",
	"salt-home-services",
	"sandbox",
	"sandlot-co",
	"saota",
	"savas-labs",
	"savvital",
	"sawyer-staffing",
	"sbc-performance",
	"scandinavian-building-services",
	"scarpel-telecom",
	"scb-techx-co-ltd",
	"school-pro-k12",
	"scientific-safety-alliance",
	"scimitar-inc",
	"sd-solutions",
	"sdv-construction-inc",
	"sealing-technologies-inc",
	"seasats",
	"securespace-self-storage",
	"security-dm-ltd",
	"seeknow",
	"segal-mccambridge",
	"selectstar-solutions",
	"sense-engineering",
	"sentinel-blue",
	"sentinel-devices",
	"sentral",
	"sepsis-alliance",
	"serato-limited",
	"serent-capital",
	"serious-development",
	"serv-recruitment-agency",
	"serve-club",
	"serverless-guru-llc",
	"servers-com",
	"seven-retail",
	"seventh-dimension",
	"shake-smart-inc",
	"share",
	"share-local-media",
	"sharesource",
	"shiny-go-clean",
	"shiphero",
	"shipscience",
	"shoals-club",
	"shopritex",
	"showami",
	"shyftoff",
	"sideworx-connect-usa",
	"sigital-llc",
	"signal-group",
	"sikhri",
	"simera-sense",
	"simple-organic-beauty",
	"sinai",
	"sine-education",
	"singlefile",
	"skirball-cultural-center",
	"skyspecs",
	"small-potato-trucking",
	"smallpartsinc",
	"smart-city-locating",
	"smart-codes-eae513730598",
	"smart-role",
	"smartmessage",
	"smartrend-manufacturing-group",
	"smbhd",
	"smileland-dental",
	"snake-oil-cocktail-company",
	"snappycx",
	"social-discovery-ventures",
	"social-nature",
	"socradar",
	"softab",
	"software-secured",
	"sola-kids-dental",
	"solace-elixirs-llc",
	"solen-software-group",
	"soles4souls",
	"soliant-consulting",
	"soligo",
	"solugen",
	"solvative",
	"sonno-malaysia",
	"sourcefit",
	"south-mountain-company",
	"southern-seminary",
	"spacewell",
	"spark-power",
	"sparkd",
	"sparks-financial",
	"speedwell-construction",
	"spinrite",
	"sponsorunited",
	"sportradar",
	"sports-reference-llc",
	"spotterrf",
	"srs-merchandising",
	"ssg-corp",
	"ssg-cr",
	"ssg-philippines",
	"ssg-texas",
	"st-amant",
	"st-julian",
	"st-mark-s-school",
	"stack-influence",
	"stake",
	"startup-wonders",
	"startupblink",
	"stateline-family-ymca",
	"statusphere",
	"step-up-team",
	"sterling-fleet-outfitters",
	"stinson",
	"storage-scholars",
	"strata-g-llc",
	"strategic-systems-international",
	"stratis-group-llc",
	"stratos-solutions",
	"streamhub",
	"subscribe",
	"sumeru-equity-partners",
	"summit-integrated-systems",
	"sunday",
	"sunday-health",
	"sunpower",
	"sunrise-landscape",
	"suny-clinton-clinton-community-college",
	"super-heat-fitness",
	"superbolt",
	"supercare-health",
	"superdispatch",
	"surge",
	"surge-institute",
	"surrey-choices",
	"survivor-healthcare",
	"sva",
	"sway",
	"sweat-dc",
	"sweetfishmedia",
	"switchboard",
	"swivl",
	"swyft-filings",
	"synergy-inc",
	"syteca",
	"t-capital",
	"t3-services-group",
	"tactibit-technologies-llc",
	"talent-acquisition-concepts",
	"talent-first",
	"talent-venture-group",
	"talentc",
	"talentmovers",
	"talk-recruitment-limited",
	"talksure",
	"tanzanian-children-s-fund",
	"tarion",
	"tau-six",
	"tawk-to",
	"teal",
	"team-201",
	"teangle",
	"technologix",
	"telespazio-be",
	"telos-media",
	"telus-agriculture",
	"telus-health-care-centers",
	"tempo-libero-cooperativa-sociale-onlus",
	"ten-canada",
	"tenor-health-foundation",
	"terrestris-global-solutions",
	"terzo-enterprises",
	"test-yantra-eu-2",
	"testbox",
	"the-abyssinian-baptist-church",
	"the-assistant",
	"the-berkeley",
	"the-bureau",
	"the-c2-group",
	"the-chateau",
	"the-clear-brands",
	"the-code-zone",
	"the-commonwealth-fund",
	"the-croghan-colonial-bank",
	"the-dyrt",
	"the-educator-s-room",
	"the-english-school",
	"the-hoth",
	"the-international-data-organization-for-transport",
	"the-luminos-fund",
	"the-matchbox-studio",
	"the-pros-weddings",
	"the-san-francisco-standard",
	"the-story-church",
	"the-sugrue-group-llc",
	"the-sutherland-group",
	"the-swim-squad",
	"the-wisdom-teeth-guys",
	"thecrossing",
	"thefaculty",
	"thementoringalliance",
	"themis-insight",
	"therapy-associates",
	"thesummitinstitute",
	"theta",
	"thirdandgrove",
	"thirdchannel-inc",
	"thoroughbred-express",
	"thp",
	"thrasher",
	"threatscape",
	"thrive-by-5",
	"thronelabs",
	"tie-bar",
	"tier-9-game-studios-ltd",
	"tiktak",
	"tippmann-group",
	"togetherhood",
	"toker-s-guide",
	"tonti-properties",
	"totalsecurityltd",
	"totalstay",
	"totara-learning-solutions",
	"tpaction",
	"trailmix",
	"transact-campus",
	"transak-inc",
	"transparent-hiring",
	"travellab",
	"tread-athletics",
	"treeline",
	"tri-county-mennonite-homes",
	"triad-electronic-technologies",
	"trialfacts",
	"tribegaming",
	"triocompany",
	"triple-cities-network-solutions",
	"trochu-motors",
	"trueline",
	"truepoint-communications",
	"truleo",
	"truplace",
	"trustpoint",
	"tsu",
	"turaco",
	"turboden",
	"turner-mining-group",
	"turnerstaffing",
	"turning-point-usa",
	"tuva",
	"tuyo",
	"twacareer",
	"twelve-consulting-group",
	"tyton-partners",
	"uclawsf",
	"udem",
	"umai",
	"uncle-mike-s",
	"under-armour",
	"uniconcepts",
	"united-home-experts-inc",
	"uniting-capital",
	"unlayer",
	"unlimit-ventures",
	"upsa",
	"upskiller",
	"upsourced-accounting",
	"upstairs",
	"uptech",
	"upwardsdotcom",
	"upwell-revenue-software-inc",
	"urban-legend",
	"urbana",
	"urrly",
	"us-digital-response",
	"us-inspect",
	"us-service-animals",
	"uvation",
	"ux-woman",
	"v-thru",
	"vadesk",
	"vald",
	"valital-technologies",
	"valley-veterinary-clinic",
	"value-store-it-management",
	"value-virtual-assistants",
	"vanguard-college-prep",
	"vanguard-ip",
	"vantage-point-solutions-inc",
	"vaporus-inc",
	"vawaa",
	"veev",
	"velomedi",
	"velox",
	"venture-forge",
	"verdes-cannabis",
	"verity-ag",
	"vert-environmental",
	"veta-virtual-inc",
	"vetro-fibermap-inc",
	"vetsez",
	"vetster",
	"viamo-inc",
	"vianai-systems",
	"victory",
	"village-car-company",
	"virtual-construction-assistants",
	"visits",
	"visolis",
	"vitalcheck-wellness",
	"vitality-hospice",
	"vitra-health",
	"vivint",
	"vivo-missouri",
	"vm-drilling-pty-ltd",
	"voiceflow",
	"voices-advance-llc-dba-anthropology-arts",
	"vontive",
	"vosyn",
	"vote-online",
	"voteamerica",
	"voters-of-tomorrow",
	"votingworks",
	"voxdata",
	"voyagu",
	"vsa",
	"vvater-llc",
	"vyde",
	"vyntra",
	"wagpco",
	"waiter-com",
	"walkerhughes",
	"warhorsestudios",
	"warren-wilson",
	"warrior-coal-llc",
	"water-works-engineers",
	"wavelength-strategy",
	"wavetronix",
	"waylin-partners-llc",
	"wayne-memorial-hospital",
	"wayy-llc",
	"we-are-futures",
	"wealthy-recruiting",
	"wearlinq",
	"weassist-io",
	"webox-jobs",
	"weco-hospitality",
	"wega-informatik-ag",
	"weight-doctors",
	"welld-sagl",
	"wersirius",
	"westairgroupofcompanies",
	"westlight-ai",
	"westphalia-holdings",
	"westrocon",
	"wevideo",
	"wheelsonsite-usa",
	"whirl-i-gig",
	"william-thomas-digital-inc",
	"winedirect",
	"winter-environmental",
	"wise-choice",
	"wmc",
	"wolf-games",
	"wongnai-media-co-ltd",
	"words-first-ltd",
	"wsna",
	"wurk",
	"xl-batteries",
	"xpansehr",
	"xtream-adminz",
	"yalla-fel-sekka",
	"yallaplay",
	"yamaha-motor-ventures-laboratory-silicon-valley",
	"yay-lunch",
	"yellow-labs-software-inc",
	"yiyienglish",
	"ymca-of-central-texas",
	"yokly",
	"young-basile",
	"your-money-line",
	"youthfully",
	"yrefy-llc",
	"zact-inc",
	"zantech-it",
	"zeal",
	"zeeks-pizza",
	"zendar",
	"zeno-power",
	"zero-g",
	"zero-hash",
	"zero-prime",
	"zetier",
	"zifty",
	"zinier",
	"zl-technologies-inc",
	"zwitterco",
}

// breezyMaxPositions bounds how many positions one board may contribute.
//
// The endpoint is unpaginated, so the loop below cannot run away the way the
// offset-based adapters did — but the response is a single JSON array with no
// declared length, and the largest board measured on 2026-07-28 ("nexthire")
// returned 2,343 positions in a 1.7 MB response. A malformed or hostile array
// is the only remaining way one tenant could consume the crawl, so the yield
// loop stops here and reports it rather than growing without limit.
const breezyMaxPositions = 20_000

// Breezy fetches one Breezy HR board.
func Breezy(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.breezy.hr/
	// https://$company.breezy.hr/json
	// https://$company.breezy.hr/p/$id-$slug
	return func(yield func(*internal.JobPosting, error) bool) {
		boardURL := "https://" + company + ".breezy.hr/json"

		board, err := fetchJSON[breezyBoard](ctx, httpClient, "Breezy", company, jsonRequest{URL: boardURL})
		if err != nil {
			yield(nil, err)

			return
		}

		if board.Positions == nil {
			yield(nil, fmt.Errorf("unexpected response shape from Breezy for company %q at %s: the body was neither a positions array nor an object carrying a %q key", company, boardURL, "positions"))

			return
		}

		positions := *board.Positions
		yielded := 0

		for index, position := range positions {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			if index >= breezyMaxPositions {
				yield(nil, fmt.Errorf("refusing to read more of the Breezy board for company %q at %s: it listed more than %d positions", company, boardURL, breezyMaxPositions))

				return
			}

			title := strings.TrimSpace(position.Name)
			postingURL := strings.TrimSpace(position.URL)

			if title == "" || !strings.HasPrefix(postingURL, "https://") {
				continue
			}

			posting := &internal.JobPosting{
				Company:  company,
				URL:      postingURL,
				Title:    title,
				Location: breezyLocation(position),

				Compensation: breezyCompensation(position.Salary),
				Department:   strings.TrimSpace(position.Department.String()),
				PostedAt:     breezyTime(position.PublishedDate),
				ExternalID:   breezyExternalID(position),
				Source: internal.PostingSource{
					Platform: breezyPlatform,
					Key:      company,
				},
			}

			// Carried only when Breezy says the location is remote. The flag is
			// per-location and there is no value meaning "office required", so its
			// absence says nothing at all; storing false would switch off the
			// location-text fallback in [internal.JobPosting.IsRemote] for the
			// whole platform, which is the mistake workable.go documents.
			if breezyIsRemote(position) {
				remote := true

				posting.Remote = &remote
				posting.WorkplaceType = internal.WorkplaceTypeRemote
			}

			// An unrecognised spelling leaves the field empty rather than
			// guessing: a wrong employment type cannot be told apart from a right
			// one by a filter, while an absent one is visibly absent.
			if employment, ok := internal.NormalizeEmploymentType(position.Type.String()); ok {
				posting.EmploymentType = employment
			}

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// A board full of positions that produced none at all means every one of
		// them was missing a name or an https URL, which no live board does. It is
		// the signature of a renamed field, and reporting zero postings for it
		// would be indistinguishable from a company that is not hiring.
		if len(positions) > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from Breezy for company %q at %s: %d positions decoded but none carried both a name and an https URL", company, boardURL, len(positions)))
		}
	}
}

// breezyBoard is one tenant's whole open-req list.
//
// Breezy has served this endpoint in two shapes. The current one, and the only
// one measured live on 2026-07-28, is a bare JSON array of positions; the older
// one is an object {"company": ..., "positions": [...]}. Both are accepted,
// because a vendor that has rolled this response once can roll it back, and an
// adapter that decodes only the current shape would answer with zero postings
// rather than an error — the silently-empty failure this project treats as its
// worst.
//
// Positions is a pointer so that "the key was missing" and "the board is empty"
// stay distinguishable: nil is a shape change and an error, a non-nil empty
// slice is a company that is not hiring today, which docs/adding-a-source.md is
// explicit is not a failure.
type breezyBoard struct {
	Positions *[]breezyPosition
}

// UnmarshalJSON accepts either shape described on [breezyBoard].
func (b *breezyBoard) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		var positions []breezyPosition
		if err := json.Unmarshal(data, &positions); err != nil {
			return err
		}

		b.Positions = &positions

		return nil
	}

	var wrapped struct {
		Positions *[]breezyPosition `json:"positions"`
	}

	if err := json.Unmarshal(data, &wrapped); err != nil {
		return err
	}

	b.Positions = wrapped.Positions

	return nil
}

// breezyPosition is one opening on a Breezy board.
//
// Only the fields this adapter publishes are modelled, per
// docs/adding-a-source.md. Two of them differ from the shapes
// docs/research/ats-platform-survey.md recorded, and both differences were
// measured rather than assumed — see [breezyNamed] for "type" and "department".
// A third, "category", is listed by the survey and is modelled by nothing here
// because no live board sent it: the key was absent from every position of every
// board captured on 2026-07-28, whose positions carry exactly the eleven keys
// id, friendly_id, name, url, published_date, type, location, locations,
// department, salary and company.
type breezyPosition struct {
	// ID is Breezy's own posting id, a 12-character hex string. It outlives the
	// URL, which a re-titled position changes, and URL-keyed [internal.Dedupe]
	// cannot follow that.
	//
	// LegacyID is the same value under the name the older response shape used.
	// It is decoded for the same reason both board shapes are accepted, and is
	// only read when ID is empty.
	ID       string `json:"id"`
	LegacyID string `json:"_id"`

	// Name is the job title. Breezy calls it "name", not "title".
	Name string `json:"name"`

	// URL is the public posting page, https://<slug>.breezy.hr/p/<id>-<slug>.
	URL string `json:"url"`

	// PublishedDate is RFC 3339 with milliseconds, "2026-07-10T19:56:44.147Z".
	PublishedDate string `json:"published_date"`

	// Type is the employment type, normalized rather than stored raw.
	Type breezyNamed `json:"type"`

	// Department is the org unit. It is frequently null or an empty string:
	// across the boards measured on 2026-07-28 a large minority of positions
	// carry no department at all.
	Department breezyNamed `json:"department"`

	// Location is the primary work location. Locations is the full list for the
	// tenants that enable multiple locations per position, and always repeats
	// the primary one, so Location alone is enough for the single-string
	// [internal.JobPosting.Location] and Locations is read only for its remote
	// flags.
	Location  breezyLocationValue   `json:"location"`
	Locations []breezyLocationValue `json:"locations"`

	// Salary is the employer-published pay, already rendered by Breezy as a
	// human string such as "$170,000 – $300,000 / year". There is no structured
	// min/max anywhere in this response, so [breezyCompensation] has to read the
	// string, and the empty string is what a tenant that does not publish pay
	// sends.
	Salary string `json:"salary"`
}

// breezyNamed decodes a field Breezy has published both as a bare string and as
// an object carrying a "name".
//
// docs/research/ats-platform-survey.md records "type" and "department" as plain
// strings. Measured against live boards on 2026-07-28 that is right for
// "department" (string, or null) and wrong for "type", which arrives as
// {"id":"fullTime","name":"Full-Time"} on every position of every board checked.
// Independent Breezy readers guard "department" with an isinstance check, which
// is what a field that is sometimes an object looks like from the outside, so
// both fields use this type: modelling either as a Go string would make one such
// tenant fail to decode, and fetchJSON decodes the whole response at once, so a
// single odd value would cost that company every posting it has.
type breezyNamed struct {
	name string
}

// String returns the human-readable value, which is the object's "name" for the
// object form and the value itself for the string form.
func (n breezyNamed) String() string { return n.name }

// UnmarshalJSON accepts a string, an object with a "name", or null.
func (n *breezyNamed) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	if trimmed[0] == '"' {
		return json.Unmarshal(data, &n.name)
	}

	var object struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}

	n.name = object.Name

	return nil
}

// breezyLocationValue is one work location on a position.
//
// Name is Breezy's own rendering, "Palo Alto, CA" or "London, England", and is
// preferred over rebuilding a string from the parts because it is what the
// employer sees on their own board. City is decoded as a fallback for the
// positions that carry no name, and is nullable — the survey records
// location.city as a string, and live boards send JSON null for it on
// remote-only positions.
//
// Country is an object with its own name, which is the survey's one location
// claim that measurement confirms exactly.
type breezyLocationValue struct {
	Name    string `json:"name"`
	City    string `json:"city"`
	Country struct {
		Name string `json:"name"`
	} `json:"country"`
	IsRemote bool `json:"is_remote"`
}

// breezyLocation renders the position's location the way its own board does.
func breezyLocation(position breezyPosition) string {
	if name := strings.TrimSpace(position.Location.Name); name != "" {
		return name
	}

	// No "name" on the primary location: rebuild the coarsest honest string from
	// the parts that are present, rather than reporting nothing.
	parts := make([]string, 0, 2)

	for _, part := range []string{position.Location.City, position.Location.Country.Name} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}

	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}

	// A position with no location at all but the remote flag set is remote-only,
	// and saying so is better than an empty string. [internal.JobPosting.Remote]
	// is set separately from the same flag.
	if breezyIsRemote(position) {
		return "Remote"
	}

	return ""
}

// breezyIsRemote reports whether Breezy marked any of the position's locations
// remote.
//
// Any rather than all: a position offered in one office and also remotely is one
// a remote candidate can take, and [internal.JobPosting.Remote] answers "may
// this be done remotely", not "is this remote-only".
func breezyIsRemote(position breezyPosition) bool {
	if position.Location.IsRemote {
		return true
	}

	for _, location := range position.Locations {
		if location.IsRemote {
			return true
		}
	}

	return false
}

// breezyCompensation reads Breezy's pay string.
//
// The value is an employer-published field rather than prose scraped out of a
// description, so it is reported as [internal.ProvenanceEmployer] — but Breezy
// publishes only the rendered string, never a structured min/max, so the figures
// themselves still have to be read out of text.
//
// [internal.ParseCompensationFromText] is deliberately reused for that instead
// of a Breezy-specific number parser. It already carries the currency detection,
// the plausible-wage bounds and the range-ratio guard that stop a stray figure
// being published as a salary, and all of those matter here: one live board
// sends "$150 – $225,000 / year", which that guard correctly rejects as two
// unrelated numbers. The parser needs a pay cue near the figures before it will
// accept them, which a bare "$50,000 – $60,000 / year" does not have, so the cue
// is supplied here — this field is by definition a salary, which is precisely
// the fact the cue encodes.
//
// The board's own rendering is kept in Summary because it can carry detail the
// numeric range cannot, such as an equity or commission note.
func breezyCompensation(salary string) *internal.Compensation {
	salary = strings.TrimSpace(salary)
	if salary == "" {
		return nil
	}

	compensation := internal.ParseCompensationFromText("Salary: " + breezyPeriodWording.Replace(salary))
	if compensation == nil {
		// Text with no figures this project is willing to stand behind:
		// "Competitive", or a range whose ends are implausible. Nothing numeric
		// is published for it, and the raw string alone is not worth a
		// compensation record.
		return nil
	}

	compensation.Summary = salary
	compensation.Provenance = internal.ProvenanceEmployer

	return compensation
}

// breezyPeriodWording rewrites the pay period Breezy renders as "/ month" into
// wording [internal.ParseCompensationFromText] recognises.
//
// Breezy always separates the figures from the unit with a spaced slash, and the
// parser's period markers are all written without that space: "/hour", "per
// hour", "hourly". So no Breezy pay string has ever set a period, and every
// figure fell through to the magnitude heuristic in
// [internal.Compensation.effectivePeriod], which calls anything at or under 250
// hourly and anything above it annual.
//
// That heuristic is right for the hourly and annual strings, which are the bulk
// of the platform, and wrong for the rest. Measured on 2026-07-28 by fetching
// the board feed of all 774 Breezy tenants that publish pay: of 14,335 salary
// strings, 6,917 end "/ hour" and 5,909 "/ year" — but 567 end "/ month", 215
// "/ week" and 95 "/ day". A live "$40 – $60 / day" was read as an hourly rate
// and published as $83,200–$124,800 a year, an 8x overstatement carrying
// [internal.ProvenanceEmployer]; that is the same failure, on the same unit,
// that dailyMarkers was added to compensation_text.go to stop in prose.
//
// The rewrite is applied only to the copy handed to the parser. Summary keeps
// the board's own rendering, because that is what the employer published.
//
// The general fix belongs in the marker lists in internal/compensation_text.go,
// which would also reach the 7,227 SuccessFactors and 9,450 Jibe pay records
// that reach this project with no period. This is the Breezy-local half.
var breezyPeriodWording = strings.NewReplacer(
	"/ hour", "per hour",
	"/ year", "per year",
	"/ month", "per month",
	"/ week", "per week",
	"/ day", "per day",
)

// breezyTime parses Breezy's published_date, which is RFC 3339.
//
// A value that does not parse yields the zero time rather than an error: a
// posting with an unreadable date is still a posting, and the zero value already
// means "the board did not say" to [internal.Filter.PostedSince].
func breezyTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}

	return parsed.UTC()
}

// breezyExternalID returns the ATS's own id for the position, preferring the
// current field name over the one the older response shape used.
func breezyExternalID(position breezyPosition) string {
	if id := strings.TrimSpace(position.ID); id != "" {
		return id
	}

	return strings.TrimSpace(position.LegacyID)
}
