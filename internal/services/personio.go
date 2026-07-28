package services

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// personioPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const personioPlatform = "personio"

func init() {
	registerBuiltin(personioPlatform, multiJobsFuncNamed(Personio, PersonioCompanies, personioCompanyName))
}

// personioMaxFeedBytes bounds one tenant's feed.
//
// The XML carries every posting's full multi-section HTML description, so it is
// the largest per-tenant response in this wave by an order of magnitude, and a
// crawl of hundreds of tenants runs them concurrently. The limit is a guard
// against one pathological tenant, not a target: the largest annotated tenant in
// the candidate list publishes about 190 openings, which is far below this even
// with generous descriptions.
const personioMaxFeedBytes = 32 << 20

// PersonioCompanies holds the Personio career sites this project crawls.
//
// An entry is normally the bare subdomain label — "holidu" is
// https://holidu.jobs.personio.de — but it may also be a full host for the
// tenants that publish on .com instead of .de. Anything containing a dot is
// treated as a host verbatim, and nothing else can be: a Personio subdomain is
// a single DNS label, so a dot in a key cannot be part of one. That is what
// keeps a .com tenant from needing an adapter change, see [personioHost].
//
// Personio is the dominant DACH/EU SMB HR suite, and its career sites publish a
// keyless XML feed carrying department, employment type, schedule, seniority and
// a creation date for every open req in a single GET. This project's coverage of
// the German-speaking mid-market is otherwise close to nil.
//
// # This list is measured, not staged
//
// All 999 slugs in testdata/candidates/personio_slugs.txt were probed live on
// 2026-07-28, each at https://<slug>.jobs.personio.de/xml?language=en and, when
// that did not answer with a feed, at the .com host:
//
//   - 968 answered HTTP 200 with a <workzag-jobs> document holding at least one
//     <position> carrying both an id and a name. All 968 are registered.
//   - 25 answered with a well-formed feed holding no positions. Those stay in
//     the candidate file, except the two that were already registered
//     ("circula", "deepset"), which are kept: docs/adding-a-source.md is
//     explicit that a company not hiring today is not a broken source.
//   - 6 are dead — rocycle, jotelulu, clikalia, cobee, globus-ai and welevel —
//     and they are dead in a specific way worth knowing about. A Personio
//     tenant that no longer exists does not 404. It answers HTTP 307 to
//     https://personio.com, on both .de and .com. See [personioFeedDocument],
//     which now refuses to follow that.
//
// The 968 live tenants published 11,920 postings between them at probe time,
// about 12 per HTTP request — the thinnest lane in this wave per request, and
// still worth it, because almost none of these employers are reachable through
// any other adapter here.
//
// docs/research/ats-platform-survey.md says "some tenants are .com not .de".
// Measured, it is two of 999: "lnds" and "weclapp", registered below as full
// hosts. Note that internal/httpx groups only *.jobs.personio.de onto a shared
// limiter key, so those two fall through to the generic per-host policy.
//
// "personio" is Personio's own board. It is small and it is kept on purpose: it
// is the tenant most likely to still exist and to still speak this format after
// a vendor change, which makes it the canary for a feed-shape break.
var PersonioCompanies = []string{
	"1komma5grad",
	"1nce",
	"360t",
	"42watt",
	"900grad-steuerberatung",
	"9elements",
	"aam2core-holding-ag",
	"academediaeducation-gmbh",
	"academia-holding-gmbh",
	"accounto-ag",
	"accure",
	"acto",
	"acura-aka",
	"ada",
	"admi",
	"adnomaly-technologies-gmbh",
	"adsquare",
	"advancis",
	"aesir",
	"aevoloop",
	"affalterbach-racing",
	"agile-robots-se",
	"agital",
	"agnosconet",
	"agora-thinktanks",
	"aimconsulting",
	"aimplas",
	"aionbank",
	"airmo",
	"akademisches-bildungs-center",
	"aktion-mensch",
	"alasco",
	"albaberlin",
	"albrings-mueller-ag",
	"allane",
	"allgeier-inovar-gmbh",
	"almanac-hotels",
	"aloha-living-immobilien",
	"ambifox",
	"ambratec-group",
	"ambrosys",
	"amnesty-international-deutschland",
	"ampack",
	"amsilk",
	"andersinnovations",
	"anhalt",
	"anqa-itsecurity-de",
	"anton",
	"antoni",
	"anyline-gmbh",
	"aok-connect",
	"apc-ag",
	"apelos",
	"apheris",
	"apollo-private-wealth",
	"appliedai",
	"apploft",
	"appsfactory-gmbh",
	"arethia",
	"aristo",
	"armedangels",
	"armira-beteiligungen-gmbh-co-kg",
	"arqis",
	"arsenal",
	"artnight-gmbh",
	"asb-rv-bergisch-land",
	"asellerate-gmbh",
	"asg",
	"asgoodasnew-electronics-gmbh",
	"ass",
	"atania",
	"atd-gmbh",
	"athereon",
	"atipik-sa",
	"atmosfair",
	"audi-formula-racing",
	"augprien",
	"augustin-beck-gmbh-co-kg",
	"ausbildung-de",
	"autohaus-royal",
	"autohaus-zemke",
	"auvesy-mdt-holding-gmbh",
	"auxalia",
	"auxmoney-gmbh",
	"avantgarde",
	"avega",
	"avidly-design-bu",
	"avow",
	"axenta-ag",
	"azeti",
	"b-plus",
	"ba-tax-gmbh",
	"badener-kurbetriebe",
	"bam-interactive",
	"bauer-elektroanlagen",
	"bauhauserde",
	"baupal-gmbh",
	"bayer-staatsbad-bad-kissingen-gmbh",
	"bb-hotels-gmbh",
	"bbb",
	"bees-bears-gmbh",
	"bell-flavors-fragrances-gmbh",
	"benefit-partner-gmbh",
	"bergmanclinics",
	"berlin-cuisine",
	"bertram",
	"bestway-deutschland",
	"bethanien-kinderdoerfer",
	"betterdoc",
	"bettermarks",
	"bewunder",
	"beyond-imaging",
	"bfgroup",
	"bfv",
	"bidt",
	"bilden-tagen-bistum-mainz-gmbh",
	"bimani",
	"biologx",
	"bit-consulting",
	"bitcap",
	"bitterpower-gmbh",
	"blackwave",
	"blazejewski-medi-tech-gmbh",
	"blue-marine-foundation",
	"bold-epic-gmbh",
	"bookit",
	"bounce-insights",
	"bounti",
	"brandl-talos",
	"brandung",
	"bremer-pflegekreis",
	"britishrowing",
	"brumaire",
	"bryck",
	"btc-echo-gmbh",
	"bucher-group",
	"buchner-partner-gmbh",
	"buddyfit",
	"buerklin-gmbh-co-kg",
	"buhl-data-service-gmbh",
	"buildhollywood",
	"building-radar",
	"bulex-rechtsanwaltsgesellschaft",
	"bund",
	"burmeister",
	"byteclub",
	"c1-green-chemicals-ag",
	"cabify",
	"cadac-group",
	"cambrium",
	"capmo",
	"cardano-foundation",
	"careloop",
	"carlnann",
	"carvia-gmbh",
	"catalym",
	"cenosco",
	"center",
	"cfl-cargo-deutschland-gmbh",
	"channel-pilot-solutions-gmbh",
	"charlesundcharlottegmbh",
	"chartworld",
	"chp",
	"christoph-dornier-klinik",
	"chrono24",
	"cinemo",
	"circula",
	"civey-gmbh",
	"ckm-group",
	"clarius",
	"clark",
	"climate",
	"climatepartner-gmbh",
	"cloover",
	"clyso-gmbh",
	"codered",
	"codex-partners",
	"coeo-at-ch",
	"coinmerce",
	"comparis-ch",
	"compipower-gmbh",
	"complori",
	"condo-group",
	"consileon",
	"convini-deutschland-gmbh",
	"coppen",
	"correctiv",
	"covermanager",
	"cps-group",
	"crate-io",
	"crisalix-labs-slu",
	"cronofy",
	"csz",
	"cuculus-gmbh",
	"cycle",
	"cycle0",
	"cyens-centre-of-excellence",
	"cyted",
	"d-o-b-landtechnik-ag",
	"dachs-it",
	"dah-gruppe",
	"dammannworks",
	"data4life",
	"datamaran",
	"dayone",
	"dbs",
	"dbwv",
	"dci",
	"ddock",
	"deecoob",
	"deepdrive-gmbh",
	"deepset",
	"deepup",
	"deinzer-weyland-gmbh",
	"deiser-bau-gmbh",
	"dela-lebensversicherungen",
	"deltavision",
	"dembach-goo-informatik-gmbh-co-kg",
	"demicon",
	"denkwerk-gmbh",
	"depoly",
	"deskbird",
	"desotec",
	"dfb",
	"diasys",
	"didit",
	"dieter-kaltenbach-stiftung",
	"digitec-gmbh",
	"dishdigital",
	"docuware",
	"dorda-rechtsanwaelte-gmbh",
	"dornier-group",
	"dosmatix-gmbh",
	"dpa",
	"dr-kittl-partner",
	"dr-schmidt-und-partner",
	"drimco",
	"drive-consulting",
	"driveblocks",
	"drk-kreisverband-guestrow-e-v",
	"ds-heibad-rec",
	"dtcf",
	"duh-group-gmbh",
	"dunia",
	"dymatrix",
	"dynamix",
	"e-lyte",
	"e2n",
	"earnesto",
	"easy2cool-gmbh",
	"ecdb-gmbh-latest",
	"ecfr",
	"ecoligo",
	"ecoplanet-green-operations-gmbh",
	"ecovery-gmbh",
	"ecpmf-sce-mbh",
	"edeka-tonscheidt",
	"ediundsepp",
	"edri",
	"education-partners",
	"eerstelijnszorg-zoetermeer",
	"efeso-management-consultants-dach",
	"egruppe",
	"egym",
	"ehret-klein-gmbh",
	"einhundert-energie-gmbh",
	"elaflex-group",
	"elainesworld",
	"element61",
	"elgin-energy",
	"elkaso",
	"emil-group-gmbh",
	"emil-kiessling-gmbh",
	"empira",
	"emuca",
	"energiequelle-gmbh",
	"enovetic",
	"entityx",
	"envelio",
	"epi-use",
	"eqs-group",
	"eraneos",
	"erlebe-fernreisen",
	"esa-grimma",
	"eurimgroup",
	"euroleague-entertainment-services-slu",
	"europ-assistance",
	"europcell",
	"ev-diakonieverein-berlin-zehlendorf-e-v",
	"evercom",
	"everphone",
	"evidentiq",
	"ewimed-gmbh",
	"excellent-air",
	"exnaton-ag",
	"exolaunch",
	"extoll-gmbh",
	"f-c-hansa-rostock",
	"f-h-bertling",
	"faceland",
	"fact-finder",
	"fairphone",
	"falkemedia",
	"faqhealth",
	"fastlta",
	"feld-energy",
	"felfel",
	"fgk-clinical-research-gmbh",
	"fiantec",
	"fieldfisher",
	"filics",
	"financialcom",
	"firestart-gmbh",
	"fiz",
	"fkr-gruppe",
	"flatpay",
	"flatrock",
	"fleetfox",
	"flh",
	"floy",
	"flybotix",
	"focuseconomics",
	"forum-berufsbildung",
	"founders-foundation-ggmbh",
	"fourvenues",
	"fraisa-gmbh",
	"framen-gmbh",
	"franka-robotics",
	"franzrosa",
	"friendlycaptcha",
	"friendsurance",
	"frtg",
	"fsc",
	"ftapi",
	"future-cleantech-architects-ggmbh",
	"garla",
	"gasser-partner",
	"geiri-europe-gmbh",
	"gesis",
	"get-e",
	"getec-holding",
	"gfos",
	"gkk-partners-partg-mbb",
	"glacier",
	"gluecklichegaeste",
	"gnosis",
	"goerg-partnerschaft-von-rechtsanwaelten",
	"good-hood-gmbh",
	"goodbytz",
	"gopagroup",
	"govradar",
	"graswald-gmbh",
	"grayoak",
	"greencells-gmbh",
	"gridfuse",
	"gross-und-partner",
	"groundies",
	"grp-steuerberater-wirtschaftspruefer",
	"grupoenhol",
	"gustavogusto",
	"gwzo",
	"h2fly-gmbh",
	"habe",
	"haendlerbund-management-ag",
	"haevg-ag",
	"hafencity-hamburg",
	"hahnair",
	"hand-werk-gmbh",
	"hanseatickids",
	"hasenkamp",
	"hass-hatje-gmbh",
	"hateaid",
	"haus-tabea",
	"hedikitas",
	"heine-beisswenger",
	"heinz-lackmann-gmbh-co-kg",
	"helloinside",
	"hemro",
	"herdify",
	"hessische-krebsgesellschaft-ev",
	"highq",
	"hilscher",
	"hintsa-performance",
	"hirschen-group",
	"hitzler",
	"hofstaetter",
	"hogast",
	"holidaycheck",
	"holidu",
	"holoplot",
	"holy-technologies-gmbh",
	"holzkern",
	"homeserve",
	"horl-1993",
	"hp-stahl",
	"htgf",
	"hubject-gmbh",
	"hum-systems-gmbh",
	"humanoo",
	"huz",
	"hwp",
	"hws",
	"hygh",
	"hyimpulse-technologies-gmbh",
	"hyntelo",
	"hypatos-gmbh",
	"iakw",
	"ic-berlin-gmbh",
	"iconic-sales",
	"id-ware",
	"idealworks",
	"ifp-consulting-gmbh-co-kg",
	"igtp",
	"ihk-akademie-suedlicher-oberrhein",
	"iits",
	"ilias-solutions",
	"imaxxam",
	"imwind",
	"inbrain-neuroelectronics",
	"index-soft",
	"indivi",
	"inexogy",
	"infiniteroots",
	"infratec-gmbh",
	"init-ag",
	"innovatec-microfibre-technology-gmbh-c",
	"instagrid",
	"intelligentfood-schweiz-ag",
	"interactiveai",
	"interrogare-gmbh",
	"intigriti",
	"intilion",
	"intuity",
	"intumind",
	"invest4kids",
	"investforwomen",
	"ioki-gmbh",
	"iomx",
	"ionity-gmbh",
	"ippf",
	"ireckonu",
	"ishap",
	"island-collective",
	"isolutions",
	"isptech",
	"it-p",
	"its-gruppe",
	"iwgroup",
	"jedox",
	"jobleads",
	"joblinge",
	"jochen-schweizer-gruppe",
	"joinpolitics",
	"joliberlin",
	"jungvonmatt",
	"juskys-gruppe-gmbh",
	"k16-gmbh",
	"karl-berrang-gmbh",
	"katkin",
	"kb1",
	"kbht",
	"keelvar",
	"kern-microtechnik",
	"kfzinnungschwaben",
	"ki-macht-schule-ggmbh",
	"kibernetik-ag",
	"kindermissionswerk-die-sternsinger-e-v",
	"king-art-gmbh",
	"kiron",
	"kitarino-service-gmbh",
	"kiwi",
	"klauke-enterprises",
	"klk-kolb",
	"kmpro-muenchen-gmbh-co-kg-stbg",
	"knick-elektronische-messgeraete-gmbh-co",
	"knime",
	"knk-gruppe",
	"knowisag",
	"kolb-distribution-ltd",
	"konzepthaus-consulting-gmbh",
	"koppla",
	"korn-recycling",
	"koro-handels-gmbh",
	"kosch-klink-performance",
	"kosys",
	"kowo-mbh-erfurt",
	"kraftblock",
	"kraftling",
	"kraus-partner-unternehemensberatung",
	"kreiosspace",
	"kremer-naturtalente",
	"kugler",
	"kugu",
	"kumi-health-gmbh",
	"kunst-werke-berlin",
	"kvrn",
	"lalive-law",
	"lanch",
	"laubenhuette",
	"laudert",
	"lautsprecherteufel",
	"lavera",
	"lavita-gmbh",
	"lc-koeln-gmbh",
	"leapartners",
	"learningculture",
	"legartis",
	"leibniz-hki",
	"leonine",
	"lepaya",
	"lightguard-gmbh",
	"lindalgroup",
	"lindenglobal",
	"linkbroker",
	"liom",
	"liqui-moly-gmbh",
	"listan-gmbh",
	"littledata",
	"liveeo-gmbh",
	"lnds.jobs.personio.com",
	"lobeco-gmbh",
	"lrz",
	"lumenaza",
	"lumibit",
	"lumics-gmbh-co-kg",
	"luna-restaurant-gmbh",
	"lush",
	"m3connect",
	"m4c",
	"macaw",
	"magrathea",
	"maibornwolff",
	"mailo-ag",
	"maisenbacher-hort-partner-1",
	"maliasili",
	"marchon",
	"martin-auer",
	"marxman-advocaten",
	"matrix42",
	"maureratmosmiddlebygmbh",
	"mawa-gmbh",
	"maxima-kitchen-equipment",
	"maytoni-gmbh",
	"maz-gruppe",
	"mbgglobal",
	"mc-travel-events",
	"medermis-clinics-gmbh",
	"mediaire",
	"medien-bayern",
	"mediengruppe-bayern-gmbh",
	"mehnert",
	"meineinkauf-gmbh",
	"meinestadt",
	"meinphysio-gmbh",
	"meinunterricht",
	"meissner-gmbh",
	"meltingelements",
	"membion-gmbh",
	"membrain",
	"memodo-gmbh",
	"mentalis",
	"mercanis",
	"merz-b-schwanen",
	"meteocontrol",
	"metergrid",
	"metro-markets-gmbh",
	"metropol-immobiliengruppe",
	"michael-wessel",
	"micronova",
	"microtech",
	"miles-mobility",
	"minut",
	"mirai",
	"mlgruppe",
	"mm-software-gmbh",
	"mobiko",
	"mobisyshr",
	"monaco-freunde-gmbh-co-kg",
	"montratec-gmbh",
	"moonwatt",
	"more-in-common",
	"mosaicit",
	"motopp",
	"mp-corporate-finance",
	"mpirix-ag",
	"mst-group",
	"mtr-rechtsanwaltsgesellschaft-mbh",
	"mueller-merkle",
	"mum",
	"mutabor",
	"muuuh-gmbh",
	"mway",
	"mybacs-vertriebs-gmbh",
	"n4-4pace",
	"nachsorgeklinik-am-straussee-ggmbh",
	"native-design",
	"navax",
	"navax-software",
	"nebul-bv",
	"neofonie-gmbh-1",
	"neos",
	"neoshare",
	"netcologne-it-services-gmbh",
	"netplans",
	"netzstrategen",
	"neuhaus-consulting-gmbh",
	"neuroelectrics",
	"newflag",
	"ng-voice",
	"nicolab",
	"nobilis",
	"nordic",
	"nordic-hamburg",
	"norvestor",
	"notpla",
	"novicap",
	"now-gmbh",
	"nscon",
	"nuernbergmesse-gmbh",
	"nunatak",
	"nussli",
	"nuuenergy",
	"nuvotex",
	"nuwacom-gmbh",
	"nvbw-mbh",
	"oberender",
	"occ-assekuradeur-gmbh",
	"odonnell-moonshine",
	"oebv",
	"ohpen",
	"okapiorbits",
	"olando-gmbh",
	"omniit",
	"oms-retail",
	"oneconcepts",
	"onepage",
	"open-xchange-gmbh",
	"openproject-gmbh",
	"optimax-energy-gmbh",
	"optiply",
	"orcan-energy",
	"orderbird",
	"ore-energy",
	"orlando-capital",
	"ororatech",
	"ory",
	"otonomee",
	"ottonova",
	"outdooractive",
	"oval",
	"oxg",
	"p36",
	"pacemaker",
	"packners-gmbh",
	"pagestreet-legal-solutions-gmbh",
	"pahnke",
	"pair",
	"palettecad",
	"palmberg",
	"papair-gmbh",
	"paperandtea",
	"partscloud",
	"pasiona",
	"passion4it-gmbh",
	"pbvi",
	"pdv-systeme-gmbh",
	"peakpeak",
	"penta",
	"penzilla-gmbh",
	"pepco",
	"personalbusinessmachine-2",
	"personio",
	"pflege-de",
	"picea-biosolutions-gmbh",
	"pioneer-europe-rd-center",
	"pixel-photonics-gmbh",
	"planet-a",
	"planet-biogastechnik-gmbh",
	"planetafoods",
	"planted",
	"plantura",
	"platoapp",
	"pmx",
	"pno-group-europe",
	"polymedics-innovations-gmbh",
	"pomelo-co",
	"pp-gmbh",
	"ppcmetrics",
	"prenode",
	"prewave",
	"prima-pflege-netzwerk-gmbh",
	"prime-time-fitness",
	"printvision",
	"prinzing-elektrotechnik-gmbh",
	"procom-automation",
	"profi-engineering-systems-ag",
	"project-a",
	"project-eaden-1",
	"prokuras",
	"proliance",
	"proteindistillery",
	"proteros-biostructures-gmbh",
	"protinus-it-bv",
	"proveg",
	"proxity-gmbh",
	"publix-ggmbh",
	"qaware",
	"qcg",
	"qplix",
	"quantum-diamond",
	"quest-consulting-ag",
	"quibim",
	"quintas",
	"raceon",
	"radlabor",
	"ramp106-gmbh",
	"randstad",
	"raum",
	"rausgegangen",
	"rcslt",
	"ready2order",
	"redalpine",
	"reha-viersen-gmbh",
	"reisenthel",
	"remazing-gmbh",
	"remotecontrol",
	"renergon-international",
	"revel8",
	"reverion",
	"reviderm-ag",
	"rheinland-air-service-gmbh",
	"rindus",
	"robnicolas",
	"rocajunyent",
	"roosh",
	"rothenberger-gruppe",
	"rtc-rath-gmbh",
	"runden-group-gmbh-co-kg",
	"safe-labs",
	"safetonet-family-store",
	"sailer-gmbh",
	"saleslayer",
	"sanoptis",
	"sarcura",
	"satellite-office-gmbh",
	"savi",
	"schiller-international-university",
	"schloesserland-sachsen",
	"schluetersche-mediengruppe",
	"schmittgall",
	"schoeffel",
	"schoene-neue-kinder",
	"schwalbe",
	"screeningeagle",
	"sea-eye",
	"secida",
	"security-research-labs",
	"seebergergmbhcokg",
	"seek-development",
	"seitenbau-gmbh",
	"sevdesk",
	"sfgroup",
	"shibari-study",
	"shyftplan",
	"sidekickhealth",
	"siegfriednass",
	"sigo",
	"silesia",
	"silpion",
	"silverflow",
	"simpledcard",
	"simplifa",
	"simplycook",
	"singlequantum",
	"sinn-power",
	"sizekick",
	"skytanking",
	"slash-digital",
	"smartekarriere",
	"smg",
	"social-match",
	"socialpals",
	"solakon-gmbh",
	"solar-andresen",
	"sozialhummel-ggmbh",
	"spinnwerk",
	"sportec-solutions",
	"sportissimo",
	"spotahome",
	"spread-gmbh",
	"squer",
	"stanley-stella",
	"stark",
	"stayraus-gmbh",
	"stc-versicherungsmakler-gmbh",
	"steinhuber",
	"sti",
	"stiftung-unabhaengige-patientenberatung",
	"stilfaser-gmbh",
	"stock3-ag",
	"storymachine",
	"storytellingcompany",
	"strateco",
	"strivion-gmbh",
	"strohm",
	"student-consultant-1",
	"studibuch-gmbh",
	"stylink",
	"sulion-digital",
	"summum",
	"sungrow-emea",
	"supercode-gmbh-co-kg",
	"swissbit",
	"syde",
	"sygns",
	"synology",
	"synsero",
	"synvert",
	"taav",
	"takevalue",
	"talento-indexa-capital",
	"tamara-comolli-fine-jewelry",
	"tandler",
	"teamative",
	"teamlove",
	"teamstyria",
	"teamzukunft-ggmbh",
	"technopolis-group",
	"techquartier",
	"techspace",
	"tecvia",
	"teliogroup",
	"telluride",
	"tennders",
	"terra-infrastructure",
	"tertianum",
	"testbusters",
	"teveo-gmbh",
	"tgm-kanzlei",
	"theaterbremen",
	"theod-mahr-soehne-gmbh",
	"thermoteknix",
	"thomassabo",
	"threatfabric",
	"tiemeyer",
	"tierarztpluspartner",
	"tiki",
	"timetoact-group",
	"titan-wind-energy-germany",
	"tngtech",
	"towa",
	"tozero",
	"tradedoubler-en",
	"tradetracker",
	"tradingeu",
	"traide-ai",
	"trauringschmiede",
	"travelcircus",
	"trb-chemedica-ag",
	"tree-logistics",
	"trendtours",
	"trever",
	"tri-merge",
	"tricept",
	"trillinghellmann",
	"trimedic",
	"triple-a",
	"triplesolar-de",
	"trusteq-gmbh",
	"tuev-ai-lab",
	"twaice",
	"twentyfour-industries",
	"ubilabs",
	"ubiops",
	"ucaneo",
	"ueagggmbh",
	"ufenau-capital-partners",
	"unite-ggmbh",
	"unitedgames",
	"unitelabs",
	"univativ-group",
	"unternehmertum",
	"uptodate-ventures-gmbh",
	"urbanovagroup",
	"userlane",
	"va-q-tec-ag",
	"vaeridion",
	"valantic-df",
	"valuedesk",
	"valve",
	"vanovate-gmbh",
	"vapaus",
	"vara",
	"varm",
	"vb-group",
	"vecttor",
	"veecle",
	"verbaneum",
	"verdane",
	"verlag-klett-cotta",
	"viataurus",
	"vicegolf",
	"vicollective",
	"viehoff-gruppe",
	"viind",
	"vindelici",
	"virtual-solution-ag",
	"vision-reality",
	"visual-components",
	"vitafy",
	"viu-ventures",
	"vlp",
	"vmc-gmbh",
	"vmray-gmbh",
	"vmt-gmbh",
	"vodeno",
	"voessing",
	"vogel-gmbh-steuerberatungsgesellschaft",
	"voltfang",
	"von-der-weppen",
	"vstep",
	"vyoma-gmbh",
	"wandelbots",
	"war-child-alliance",
	"watr",
	"wattline",
	"waveit-gmbh",
	"weber-ingenieure-gmbh",
	"weclapp.jobs.personio.com",
	"wecreate-germany-gmbh",
	"wefralife",
	"welearn",
	"wellcosan-gmbh",
	"wera-werkzeuge",
	"wertemuseum",
	"wertgrund",
	"west-end-dental",
	"westwing",
	"whu-otto-beisheim-school-of-management-1",
	"widas",
	"wifor",
	"wild-balance",
	"wilken",
	"windesign",
	"windhager",
	"windotec",
	"wire-1",
	"wirtschaftsrat-der-cdu-e-v",
	"wks-technik",
	"wksimonsfeld",
	"workidentity",
	"wscad",
	"wund",
	"wunderflats",
	"wwf-belgium",
	"wwp",
	"xrxes-cc",
	"ymesg",
	"yourjobconsult",
	"zebes",
	"zellerfeld",
	"zeo-solar",
	"zeus-beteiligungs-und-beratungs-gmbh",
	"zg-zentrum-gesundheit-gmbh",
	"zim-aircraft-seating-gmbh",
	"zimex",
	"zipmend",
	"zollsoft",
	"ztk",
	"zvoove",
}

// personioHost returns the career-site host for a tenant key.
//
// A key with no dot is a subdomain label on the platform's default domain, which
// is what almost every tenant is. A key with a dot is a full host, which is how a
// .com tenant is registered without this adapter learning a per-tenant domain
// table. A key written as a URL is accepted too, because somebody will
// eventually paste one.
func personioHost(key string) string {
	host := strings.TrimSpace(key)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host, _, _ = strings.Cut(host, "/")

	if strings.Contains(host, ".") {
		return host
	}

	return host + ".jobs.personio.de"
}

// personioCompanyName derives the display name for a tenant from its key, which
// is the leading label either way.
func personioCompanyName(key string) string {
	host := personioHost(key)

	label, _, _ := strings.Cut(host, ".")

	return label
}

// personioFeed is a tenant's whole open-req list.
//
// XMLName is declared, so a response whose root element is not <workzag-jobs>
// fails to unmarshal instead of yielding zero positions. That distinction is the
// entire point: Personio's feed is a per-tenant opt-in, and a tenant that has not
// enabled it, or a subdomain that no longer exists, answers with something that
// is not this document. Decoding that into an empty list would report the source
// as a company that is simply not hiring, which docs/architecture-roadmap.md
// calls the worst failure available.
//
// The element is named for Workzag, the company Personio was founded as. It has
// outlived the rename by more than a decade.
type personioFeed struct {
	XMLName   xml.Name           `xml:"workzag-jobs"`
	Positions []personioPosition `xml:"position"`
}

// personioPosition is one opening in the feed.
//
// Only the fields this adapter publishes are modelled, per
// docs/adding-a-source.md. The <jobDescriptions> block — the entire posting
// body, in entity-encoded HTML, and the great majority of the feed's bytes — is
// deliberately not decoded: [internal.JobPosting] has nowhere to put it, and
// skipping it keeps a 32 MiB feed from becoming 32 MiB of retained strings.
type personioPosition struct {
	// ID is Personio's own posting id, and the only thing the public posting URL
	// is built from, see [personioPostingURL].
	ID   string `xml:"id"`
	Name string `xml:"name"`

	// Office is the primary location; a tenant with several publishes the rest
	// under <additionalOffices>.
	Office           string   `xml:"office"`
	AdditionalOffice []string `xml:"additionalOffices>office"`

	Department string `xml:"department"`

	// RecruitingCategory is Personio's second, independent grouping of a posting
	// ("Sales", "Tech"). It is stored as the team rather than dropped because
	// [internal.Filter.Departments] searches department and team together, so
	// whichever of the two a tenant actually fills in answers `--department`.
	RecruitingCategory string `xml:"recruitingCategory"`

	// EmploymentType is Personio's tenure vocabulary: "permanent", "intern",
	// "trainee", "freelance", "working-student". Note that it is not the
	// full-time/part-time distinction, which is Schedule.
	EmploymentType string `xml:"employmentType"`

	// Schedule is "full-time", "part-time" or "full-or-part-time".
	Schedule string `xml:"schedule"`

	// Seniority is Personio's level vocabulary: "entry-level", "experienced",
	// "student", "lead". It is stored verbatim, which is what
	// [internal.JobPosting.Seniority] is for — levelling is a per-employer
	// ladder, and canonicalising it would be this project inventing an opinion
	// about somebody else's job architecture.
	Seniority string `xml:"seniority"`

	// CreatedAt is when the posting was created, in ISO-8601 with a numeric
	// zone. It is the only date the feed carries.
	CreatedAt string `xml:"createdAt"`

	// Salary is the employer-published pay range, for the tenants that fill it
	// in.
	//
	// docs/research/ats-platform-survey.md does not mention it, and this adapter
	// shipped publishing no compensation for Personio at all. A probe of all 999
	// candidate tenants on 2026-07-28 found the element on 1,192 of 11,938 live
	// positions, fully structured: 981 with both bounds, 211 with only a
	// minimum, an ISO currency code alongside the display symbol, and a "type"
	// that is always one of "yearly" (704), "monthly" (334) or "hourly" (134).
	//
	// It is as good a source as the Ashby and Lever ranges this project already
	// trusts: a dedicated numeric field the employer filled in, not a figure
	// read out of prose, hence [internal.ProvenanceEmployer].
	Salary struct {
		Min string `xml:"min"`
		Max string `xml:"max"`

		// CurrencyCode is the ISO 4217 code. The feed also carries a
		// currencySymbol ("€", "£") which is deliberately not read: it is a
		// display glyph, and "$" alone does not name a currency.
		CurrencyCode string `xml:"currencyCode"`

		// Type is Personio's interval spelling, always adverbial here.
		Type string `xml:"type"`
	} `xml:"salaryInformation"`
}

// personioPeriods maps Personio's salaryInformation type onto [internal.Period].
// The three adverbial spellings are the only values measured live; the bare
// units are accepted too, since they cost nothing and a board that changes its
// spelling should not silently lose its periods.
var personioPeriods = map[string]internal.Period{
	"hour":    internal.PeriodHour,
	"hourly":  internal.PeriodHour,
	"day":     internal.PeriodDay,
	"daily":   internal.PeriodDay,
	"week":    internal.PeriodWeek,
	"weekly":  internal.PeriodWeek,
	"month":   internal.PeriodMonth,
	"monthly": internal.PeriodMonth,
	"year":    internal.PeriodYear,
	"yearly":  internal.PeriodYear,
	"annual":  internal.PeriodYear,
}

// personioAmount reads one of the salary bounds, reporting false when it is not
// a usable figure. They arrive as decimal strings ("30000.00", "18.50").
func personioAmount(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 {
		return 0, false
	}

	return value, true
}

// personioCompensation turns the feed's salaryInformation into a pay range,
// returning nil for the great majority of positions, which publish none.
func personioCompensation(position personioPosition) *internal.Compensation {
	comp := &internal.Compensation{
		Currency:   strings.ToUpper(strings.TrimSpace(position.Salary.CurrencyCode)),
		Period:     personioPeriods[strings.ToLower(strings.TrimSpace(position.Salary.Type))],
		Provenance: internal.ProvenanceEmployer,
	}

	if minimum, ok := personioAmount(position.Salary.Min); ok {
		comp.Min = minimum
	}

	if maximum, ok := personioAmount(position.Salary.Max); ok {
		comp.Max = maximum
	}

	// A currency with no figures is not a pay range; publishing it would make
	// --has-pay match postings that disclose nothing.
	if comp.IsZero() {
		return nil
	}

	return comp
}

// personioTimeLayouts are the shapes a Personio timestamp arrives in, most
// likely first.
var personioTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// personioTime parses Personio's createdAt into UTC, returning the zero time
// when the feed published none or the value cannot be read.
//
// An unreadable value yields the zero time rather than an error: one posting with
// an odd timestamp must not cost a board its other postings, and
// [internal.Filter.PostedSince] excludes undated postings anyway, so the failure
// mode is a posting missing from a date query rather than a wrong date in it.
func personioTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}

	for _, layout := range personioTimeLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			// Stored in UTC so comparing a Personio posting with an Ashby one is a
			// comparison of instants rather than of the zones two boards happened
			// to render in — and this platform is almost entirely CET/CEST, which
			// is an hour or two off UTC all year.
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// personioLocation renders the offices a posting is offered at.
func personioLocation(position personioPosition) string {
	names := make([]string, 0, len(position.AdditionalOffice)+1)

	for _, office := range append([]string{position.Office}, position.AdditionalOffice...) {
		if office = strings.TrimSpace(office); office != "" && !slices.Contains(names, office) {
			names = append(names, office)
		}
	}

	if len(names) == 0 {
		return "unknown"
	}

	return strings.Join(names, "; ")
}

// personioEmploymentType resolves the engagement from the two fields Personio
// publishes, which split one concept in a way no other board here does.
//
// employmentType carries tenure ("permanent", "intern", "freelance") and
// schedule carries hours ("full-time", "part-time"), so both have to be
// consulted. employmentType is tried first because "intern" and "freelance" are
// the more specific answers: an internship that is also full-time is better
// filed as an internship. "permanent" is deliberately unrecognised by
// [internal.NormalizeEmploymentType] — a permanent part-time role is ordinary —
// so those postings fall through to the schedule, which is exactly the intent.
//
// "full-or-part-time" is rejected before it reaches the normalizer. It squashes
// to a string ending in "parttime", so the normalizer would read a role open to
// either as part-time, and a filter cannot tell a wrong answer from a right one.
func personioEmploymentType(position personioPosition) internal.EmploymentType {
	if employment, ok := internal.NormalizeEmploymentType(position.EmploymentType); ok {
		return employment
	}

	if personioAmbiguousSchedule(position.Schedule) {
		return internal.EmploymentTypeUnknown
	}

	if employment, ok := internal.NormalizeEmploymentType(position.Schedule); ok {
		return employment
	}

	return internal.EmploymentTypeUnknown
}

// personioAmbiguousSchedule reports whether a schedule offers full-time and
// part-time both, in which case it constrains nothing.
func personioAmbiguousSchedule(schedule string) bool {
	lowered := strings.ToLower(schedule)

	return strings.Contains(lowered, "full") && strings.Contains(lowered, "part")
}

// personioPostingURL builds the public posting page for one position.
//
// The feed carries no link of its own, so this is synthesized — the same trick
// [successFactorsApplyURL] uses, and the reason this platform costs one request
// per employer rather than one per posting. The route is the tenant's own host
// plus "/job/{id}", which is the URL every Personio career site links its own
// postings by.
func personioPostingURL(host, id string) string {
	return "https://" + host + "/job/" + id
}

// personioFeedDocument fetches and parses one tenant's feed.
//
// It does not go through [fetchJSON]: the response is XML. The body is closed
// before this returns on every path, so a failed read cannot leave a connection
// pinned for the rest of the crawl.
func personioFeedDocument(ctx context.Context, httpClient *http.Client, company, host, feedURL string) (*personioFeed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Personio company %q at %s: %w", company, feedURL, err)
	}

	req.Header.Set("Accept", "application/xml, text/xml, */*")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Personio for company %q at %s: %w", company, feedURL, err)
	}
	defer resp.Body.Close()

	// A tenant that no longer exists, or that has not switched the feed on,
	// answers HTTP 307 to https://personio.com — the vendor's marketing site.
	// The shared client follows redirects, so without this check the crawl would
	// fetch that page, fail to parse it as XML and report a confusing error,
	// having spent the request on the wrong host.
	//
	// Which host matters. internal/httpx keys its limiter on the
	// ".jobs.personio.de" suffix, and personio.com does not match it, so every
	// dead tenant's redirect lands on a host with no policy and no shared key.
	// Six of the 999 candidates probed on 2026-07-28 redirect this way, and
	// probing them was enough to make personio.com start answering 429 —
	// self-inflicted rate limiting on a vendor's front page, which is the
	// Workable incident httpx.go:41-44 records, in a new costume. With hundreds
	// of tenants registered, tenants going dark over time would aim all of that
	// at one host. Refusing to follow the redirect off the tenant's own host
	// keeps a dead tenant a cheap, clearly-reported failure.
	//
	// The nil guard is not decoration: [http.Client.Do] always sets
	// Response.Request, but this takes any client, and a panic here would take a
	// whole crawl worker with it.
	if final := responseURL(resp); final != nil && !strings.EqualFold(final.Host, host) {
		return nil, fmt.Errorf("Personio redirected company %q from %s to %s, so this tenant does not publish a feed", company, feedURL, final.Redacted())
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from Personio for company %q at %s: %s", company, feedURL, resp.Status)
	}

	decoder := xml.NewDecoder(io.LimitReader(resp.Body, personioMaxFeedBytes))

	// Strict off, plus the HTML entity table, because the feed is XML that
	// carries HTML: descriptions arrive entity-encoded, and a single "&nbsp;"
	// that a tenant's editor left raw is enough for a strict parser to reject the
	// whole document. That is the SuccessFactors failure in a different costume
	// (see successfactors.go, where a feed that is not quite XML has to be
	// scanned rather than parsed), and it would cost an entire employer's
	// postings rather than one description this adapter does not even read.
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	feed := new(personioFeed)

	// A truncated body — a feed larger than the limit above, or a connection cut
	// mid-document — fails here as a syntax error rather than decoding into a
	// short list of positions. Half a company's postings reported as all of them
	// is precisely the silent failure this project refuses to produce.
	if err := decoder.Decode(feed); err != nil {
		return nil, fmt.Errorf("failed to decode XML feed from Personio for company %q at %s: %w", company, feedURL, err)
	}

	return feed, nil
}

// responseURL returns the URL a response was finally served from, after any
// redirects, or nil when the client did not record one.
func responseURL(resp *http.Response) *url.URL {
	if resp == nil || resp.Request == nil {
		return nil
	}

	return resp.Request.URL
}

// Personio returns all of the job postings for one Personio career site, or an
// error if there was a problem making the request or reading the feed.
//
// company is the tenant's subdomain, or a full host for the tenants that publish
// on a domain other than the default; see [PersonioCompanies].
//
// There is no pagination here, deliberately: the feed answers with the tenant's
// entire open-req list, so there is no page parameter for a board to ignore and
// no loop for [pageRepeatGuard] to bound.
func Personio(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.jobs.personio.de/
	// https://$company.jobs.personio.de/xml?language=en
	// https://$company.jobs.personio.de/job/$id
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			host        = personioHost(company)
			companyName = personioCompanyName(company)

			// language=en asks for the English rendering where the operator
			// maintains one; tenants that do not keep the posting in its own
			// language, which is the honest fallback for a platform whose
			// employers are mostly German-speaking.
			feedURL = "https://" + host + "/xml?language=en"
		)

		feed, err := personioFeedDocument(ctx, httpClient, company, host, feedURL)
		if err != nil {
			yield(nil, err)

			return
		}

		yielded := 0

		for _, position := range feed.Positions {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			var (
				id    = strings.TrimSpace(position.ID)
				title = strings.TrimSpace(position.Name)
			)

			// Without an id there is no URL to publish, since the link is built
			// from it.
			if id == "" || title == "" {
				continue
			}

			posting := &internal.JobPosting{
				Company:  companyName,
				URL:      personioPostingURL(host, id),
				Title:    title,
				Location: personioLocation(position),

				Compensation:   personioCompensation(position),
				Department:     strings.TrimSpace(position.Department),
				EmploymentType: personioEmploymentType(position),
				Seniority:      strings.TrimSpace(position.Seniority),
				PostedAt:       personioTime(position.CreatedAt),
				ExternalID:     id,
				Source: internal.PostingSource{
					Platform: personioPlatform,
					Key:      company,
				},
			}

			// Only when it says something the department does not already.
			if category := strings.TrimSpace(position.RecruitingCategory); !strings.EqualFold(category, posting.Department) {
				posting.Team = category
			}

			// Personio publishes no structured workplace field at all, so
			// WorkplaceType stays unknown and Remote stays nil. An office named
			// "Remote" is location text, and [internal.NormalizeWorkplaceType] is
			// explicit that it must not be fed one: "Remote, OR" is a town in
			// Oregon often enough that [internal.JobPosting.IsRemote]'s heuristic
			// is kept deliberately separate from a board's structured answer.
			// Leaving both empty is what lets that heuristic still run.

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// A feed full of positions that produced no postings at all means every
		// one of them was missing an id or a title, which no live board does. It
		// is the signature of a renamed element, and reporting zero postings for
		// it would be indistinguishable from a company that is not hiring.
		if len(feed.Positions) > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected feed shape from Personio for company %q at %s: %d positions decoded but none carried both an id and a name", company, feedURL, len(feed.Positions)))
		}
	}
}
