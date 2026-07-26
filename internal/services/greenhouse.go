package services

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// greenhousePlatform is the platform name this file registers under, shared with
// the [internal.PostingSource] every posting carries so the two cannot drift
// apart.
const greenhousePlatform = "greenhouse"

func init() {
	registerBuiltin(greenhousePlatform, multiJobsFunc(Greenhouse, GreenhouseCompanies))
}

var GreenhouseCompanies = []string{
	"2k",
	"2u",
	"6sense",
	"abnormalsecurity",
	"accela",
	"aceable",
	"acquia",
	"acuitymd",
	"adyen",
	"affirm",
	"agero",
	"agilityrobotics",
	"aha",
	"airbnb",
	"airtable",
	"airtamejobs",
	"akunacapital",
	"alertmedia",
	"alethea",
	"algolia",
	"alloy",
	"alpaca",
	"alphasense",
	"altoslabs",
	"altruist",
	"amperity",
	"amplitude",
	"amwell",
	"analyst1",
	"andurilindustries",
	"anthropic",
	"anydesk",
	"apaleo",
	"apiiro",
	"apolloio",
	"applecart",
	"appsflyer",
	"apptronik",
	"aqr",
	"arcinstitute",
	"arizeai",
	"arkoselabs",
	"arlosolutionsllc",
	"armada",
	"armorcode",
	"array",
	"asana",
	"atbay",
	"atbayjobs",
	"attain",
	"aura",
	"aurorainnovation",
	"automox",
	"axonius",
	"babylist",
	"bandwidth",
	"bankrate",
	"beamtherapeutics",
	"beautifulai",
	"betterment",
	"beyondfinance",
	"beyondone",
	"beyondtrust",
	"bigid",
	"billcom",
	"bird",
	"bishopfox",
	"bitgo",
	"bitpanda",
	"bitwarden",
	"blackduck",
	"blacksky",
	"blend",
	"block",
	"blockchain",
	"blumira",
	"bombas",
	"bondsmith",
	"brainpop",
	"branch",
	"branchmetrics",
	"brave",
	"braze",
	"brex",
	"bringg",
	"britive",
	"brooklinen",
	"bugcrowd",
	"builder",
	"buildkite",
	"buildops",
	"builtin",
	"bungie",
	"buzzfeed",
	"cais",
	"calendly",
	"calm",
	"cameo",
	"candid",
	"canonical",
	"capco",
	"capellaspace",
	"carbonrobotics",
	"cargurus",
	"carrotfertility",
	"carta",
	"carvana",
	"catonetworks",
	"cbinsights",
	"celonis",
	"censys",
	"cerebral",
	"chainguard",
	"chargepoint",
	"checkr",
	"cheddar",
	"chime",
	"circleci",
	"cision",
	"clara",
	"clarksoneyecare",
	"clear",
	"clearstreet",
	"cleo",
	"clever",
	"clickhouse",
	"clicktherapeutics",
	"cloudflare",
	"cloudsek",
	"cloverhealth",
	"coalition",
	"cockroachlabs",
	"codeacademy",
	"coherehealth",
	"coinbase",
	"collibra",
	"colossalbiosciences",
	"commercetools",
	"commvault",
	"consensys",
	"constantcontact",
	"contentful",
	"contentstack",
	"controlplane",
	"cookunity",
	"cordial81",
	"corelight",
	"coreweave",
	"cortex",
	"couchbaseinc",
	"coupang",
	"coursera",
	"cresta",
	"crexi",
	"cribl",
	"crossbeam",
	"crossriverbank",
	"crunchyroll",
	"current",
	"customerio",
	"cybereason",
	"cybersheath",
	"cymulate",
	"dagger",
	"dashlane",
	"databento",
	"databricks",
	"datadog",
	"datagrail",
	"dawnaerospace",
	"dbtlabsinc",
	"deepmind",
	"deepwatchinc",
	"definitivehc",
	"descope",
	"descript",
	"detroitlions",
	"devrev",
	"digible",
	"digicert",
	"digitalocean98",
	"discord",
	"dnsfilter",
	"docnetwork",
	"dominodatalab",
	"donorschoose",
	"doordashusa",
	"dots",
	"doximity",
	"dragos",
	"dremio",
	"dropbox",
	"druva",
	"dscout",
	"duda",
	"duolingo",
	"earnest",
	"earnin",
	"ebanx",
	"edb",
	"egress",
	"elastic",
	"electreon",
	"embroker",
	"endorlabs",
	"energycx",
	"enova",
	"envoyglobalinc",
	"epicgames",
	"ethoslife",
	"eve",
	"everquote",
	"expel",
	"extend",
	"faire",
	"fairlife",
	"fanduel",
	"fareharbor",
	"fastly",
	"feedzai",
	"figma",
	"figureai",
	"fingerprint",
	"fireblocks",
	"fireworksai",
	"firsthand",
	"fivetran",
	"flatironhealth",
	"flexport",
	"flourish",
	"flowtraders",
	"flumehealth",
	"flyzipline",
	"form3",
	"formationbio",
	"forter",
	"fortisgames",
	"forward",
	"fossainc",
	"fourkites",
	"fubotv",
	"garnerhealth",
	"gemini",
	"genedx",
	"generalassembly",
	"generatebiomedicines",
	"genevatrading",
	"geniussports",
	"genomenoninc",
	"gigs",
	"ginkgobioworks",
	"gitlab",
	"gleanwork",
	"glow",
	"goldenhippo",
	"goldenpetbrands",
	"gomotive",
	"gradientai",
	"gradientio",
	"gradle",
	"grafanalabs",
	"graphcore",
	"gravwell",
	"greenpeace",
	"gremlin",
	"groupon",
	"groww",
	"guidepointsecurity",
	"gumgum",
	"gusto",
	"gympass",
	"hackerrank",
	"haizelabs",
	"halcyon",
	"harnessinc",
	"hazel",
	"healthjoy",
	"hellofresh",
	"heycar",
	"hiddenlayer",
	"highnote",
	"highradius",
	"hightouch",
	"himarley",
	"hivewatch",
	"hometap",
	"honeycomb",
	"hudl",
	"hungryroot",
	"huntress",
	"hypr",
	"idme",
	"imbue",
	"imc",
	"immersive",
	"immunefi",
	"imply",
	"incode",
	"inflectionai",
	"infotrust",
	"inkind",
	"inspiren",
	"instabase",
	"instacart",
	"intercom",
	"intersystems",
	"inversionspace",
	"ionos",
	"ionq",
	"isaraerospace",
	"iterable",
	"jamf",
	"janestreet",
	"jdsports",
	"jetbrains",
	"jfrog",
	"jumptrading",
	"justmarkets",
	"justworks",
	"juullabs",
	"kairospower",
	"kalderos",
	"kaseya",
	"kayak",
	"keepersecurity",
	"kentik",
	"keyfactorinc",
	"khanacademy",
	"khealthcareers",
	"klaviyo",
	"knowbe4",
	"kodiak",
	"komodohealth",
	"labelbox",
	"lastpass",
	"launchdarkly",
	"launchx",
	"leaflink",
	"lendingtree",
	"letsgetchecked",
	"liftoff",
	"lithic",
	"lob",
	"logicgate",
	"logicmonitor",
	"lookout",
	"lucidmotors",
	"lucidsoftware",
	"lyft",
	"mabl",
	"majorleaguebaseball",
	"mangroup",
	"marqeta",
	"marshallwace",
	"mavenclinic",
	"maymobility",
	"melio",
	"mercury",
	"metronome",
	"mightynetworks",
	"mill",
	"mindsdb",
	"minio",
	"mirakl",
	"mitsubishimotorsna",
	"mixbook",
	"mixpanel",
	"mochihealth",
	"modernize",
	"mongodb",
	"monks",
	"monzo",
	"morsecorp",
	"mos",
	"motive",
	"movableink",
	"muckrack",
	"myfitnesspal",
	"n26",
	"nansen",
	"narvar",
	"navapbc",
	"nebius",
	"neo4j",
	"netlify",
	"netskope",
	"newrelic",
	"nextdoor",
	"ninjatrader",
	"nirmata",
	"nksecuritiesresearch",
	"nmi",
	"novacredit",
	"nozominetworks",
	"nuro",
	"nuvalent",
	"oasis",
	"observeai",
	"obsidiansecurity",
	"ocrolusinc",
	"octanelending",
	"octus",
	"offerup",
	"oklo",
	"okta",
	"omadahealth",
	"oneimaging",
	"onetrust",
	"openly",
	"opentable",
	"openzeppelin",
	"opswat",
	"optimal",
	"orcasecurity",
	"origisenergy",
	"oscar",
	"otter",
	"outpostspace",
	"pagerduty",
	"pagerhealth",
	"pandadoc",
	"pangeamoneytransfer",
	"pantheon",
	"pantherlabs",
	"parsleyhealth",
	"pathai",
	"patientpoint",
	"paveakatroveinformationtechnologies",
	"paxlabs",
	"payoneer",
	"peloton",
	"pendo",
	"phaidra",
	"philo",
	"phonepe",
	"piaggiofastforward",
	"pieinsurance",
	"pingcap",
	"pingidentity",
	"pinterest",
	"pinwheelapi",
	"pioneersquarelabs",
	"planetscale",
	"platformscience",
	"platformsh",
	"plume",
	"pontera",
	"postman",
	"postscript",
	"praetorian",
	"predictiveindex",
	"prolaio",
	"proshares",
	"proto",
	"proton",
	"psiquantum",
	"public",
	"publicinput",
	"pulumicorporation",
	"purestorage",
	"pushpay",
	"qualia",
	"qualtrics",
	"quanata",
	"quince",
	"radar",
	"rampnetwork",
	"razorpaysoftwareprivatelimited",
	"rdccareers",
	"reach",
	"rebelliondefense",
	"rebuilt",
	"recordedfuture",
	"recursionpharmaceuticals",
	"reddit",
	"redwoodmaterials",
	"remarkably",
	"remotecom",
	"resilience",
	"revivn",
	"rhymetec",
	"rigup",
	"riotgames",
	"ripple",
	"risalabs",
	"riskified",
	"roadie",
	"robinhood",
	"roblox",
	"rocketlab",
	"rocketreach",
	"rockstargames",
	"roku",
	"rondoenergy",
	"roofr",
	"roofstock",
	"rtbhouse",
	"rubrik",
	"runwise",
	"runzero",
	"safebreach",
	"sagent",
	"salesloft",
	"saltsecurity",
	"samsara",
	"saucelabs",
	"scaleai",
	"scandit",
	"scopely",
	"scotch",
	"scoutmotors",
	"seatgeek",
	"sendbird",
	"sentinellabs",
	"serviceexpress",
	"sezzle",
	"sgnlaiinc",
	"shakepay",
	"sigmacomputing",
	"silananotechnologies",
	"simplisafe",
	"singlestore",
	"skillsoft",
	"slice",
	"smartasset",
	"smartbear",
	"smartling",
	"smartsheet",
	"snapmobileinc",
	"snorkelai",
	"sofi",
	"solarwinds",
	"soloioinc",
	"sonyinteractiveentertainmentglobal",
	"sourcegraph91",
	"spacex",
	"specterops",
	"speechmatics",
	"spektrum",
	"spothero",
	"spotter",
	"sproutsocial",
	"spycloud",
	"squarepointcapital",
	"squarespace",
	"stabilityai",
	"stackav",
	"starburst",
	"stitchfix",
	"stockx",
	"storyblok",
	"stratolaunch",
	"stripe",
	"sumologic",
	"surveygizmo",
	"surveymonkey",
	"sweetgreen",
	"sweetsecurity",
	"synack",
	"taboola",
	"tailscale",
	"taketwo",
	"talkspace",
	"tanium",
	"taskrabbit",
	"tastytrade",
	"techholding",
	"temporaltechnologies",
	"tenableinc",
	"tenstorrent",
	"terakeet",
	"textus",
	"thefarmersdog",
	"thefloridapanthers",
	"thenewyorktimes",
	"thetradedesk",
	"tia",
	"tide",
	"tigergraph",
	"tines",
	"toast",
	"togetherai",
	"tokensecurity",
	"tomorrowhealth",
	"torcrobotics",
	"torotms",
	"torq",
	"towerresearchcapital",
	"transmitsecurity",
	"treasuryprime",
	"tripactions",
	"tripadvisor",
	"truelayer",
	"trufflesecurity",
	"trumid",
	"trustpilot",
	"truveta",
	"tulip",
	"twilio",
	"udacity",
	"udemy",
	"unanet",
	"underdogfantasy",
	"upgrade",
	"upstart",
	"urbancompass",
	"ursamajor",
	"vast",
	"vectara",
	"vectranetworks",
	"veeamsoftware",
	"vega",
	"veracode",
	"vercel",
	"verifone",
	"vestmark",
	"veterinaryemergencygroupst",
	"virtu",
	"vonage",
	"voxmedia",
	"walnut",
	"warp",
	"watershed",
	"waymo",
	"wayve",
	"webflow",
	"whop",
	"wikimedia",
	"williamblair",
	"wisetack",
	"wizeline",
	"wizinc",
	"wonderschool",
	"workato",
	"workstream",
	"wrike",
	"xai",
	"xendit",
	"yext",
	"yugabyte",
	"zerocater",
	"zetaglobal",
	"zocdoc",
	"zoo",
	"zoro",
	"zscaler",
	"zuora",
}

// greenhouseJobs is a struct that represents the JSON response from the
// Greenhouse API for job postings used internally to obtain the job postings
// for a given company.
//
// Every field decoded here is already in the plain list response, so across 647
// Greenhouse sources — the largest platform in the project — this adds no
// request and no measurable bytes. What is deliberately absent is everything
// gated behind `?content=true`: the description body, departments and offices.
// That parameter goes on this very URL, so it would cost no extra request, but
// it inflates the response about 13.7x (Databricks 0.7 MB to 9.4 MB, Stripe
// 0.3 MB to 4.0 MB) — roughly 65 MB to 900 MB across the platform. The nightly
// crawl already fails to finish inside its 75-minute budget, so that trade needs
// an explicit opt-in the adapter signature cannot yet carry, and until it exists
// Greenhouse stays on the cheap response. This is the reason Greenhouse gets no
// department and no prose-derived pay while Ashby and Lever do.
type greenhouseJobs struct {
	Jobs []struct {
		AbsoluteURL string `json:"absolute_url"`

		// ID is the board's posting id, the one in absolute_url and the key of
		// Greenhouse's per-job endpoint. internal_job_id is a different number,
		// the internal req the posting hangs off, and has no field in
		// [internal.JobPosting] to land in, so it is left undecoded.
		ID int64 `json:"id"`

		Location struct {
			Name string `json:"name"`
		} `json:"location"`

		Title string `json:"title"`

		// UpdatedAt is ISO-8601 with a numeric zone, "2024-05-01T12:00:00-04:00".
		// Greenhouse is by far the widest source of a real timestamp in this
		// project, and this is the only one the cheap response carries.
		UpdatedAt string `json:"updated_at"`

		// FirstPublished is documented on the per-job endpoint rather than on
		// this list, and is decoded opportunistically: tenants that do send it in
		// the list get a real PostedAt for free, and the rest leave it zero.
		//
		// It is emphatically not defaulted to UpdatedAt. An employer editing a
		// description does not make a nine-month-old req new, and
		// [internal.Filter.PostedSince] would then quietly fill a "posted this
		// week" query with stale postings.
		FirstPublished string `json:"first_published"`

		// RequisitionID is the employer's own req number.
		RequisitionID greenhouseScalar `json:"requisition_id"`
	} `json:"jobs"`
}

// greenhouseScalar decodes a JSON value whose type Greenhouse does not hold
// stable into a string.
//
// requisition_id is free text the employer types, and it arrives as a string
// ("JR0012345"), as a bare number (41815) and as null, all from the same API.
// Modelling it as a Go string would make every tenant that sends a number fail
// to decode, and fetchJSON decodes a whole page at once, so that single field
// would take down an entire company's postings — the silently-empty source this
// project treats as its worst failure. Anything that is not a scalar is dropped
// rather than erroring, for the same reason.
type greenhouseScalar string

// UnmarshalJSON implements [json.Unmarshaler].
func (s *greenhouseScalar) UnmarshalJSON(data []byte) error {
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

		*s = greenhouseScalar(text)

		return nil
	}

	// An object or array is not a requisition number, and rendering its literal
	// JSON into the field would publish "{...}" as an employer's req id.
	if trimmed[0] == '{' || trimmed[0] == '[' {
		*s = ""

		return nil
	}

	*s = greenhouseScalar(trimmed)

	return nil
}

// greenhouseTimestamp parses one of Greenhouse's ISO-8601 timestamps into UTC,
// returning the zero time when the field was absent or unreadable.
//
// A value that does not parse yields the zero time rather than an error: one
// posting with an odd timestamp must not cost a board its other postings, and an
// absent date merely drops the posting out of a [internal.Filter.PostedSince]
// query, which is the safe direction to fail in.
//
// Kept separate from [ashbyPublishedAt] even though both boards happen to speak
// RFC 3339 today: these are two independent third-party formats that agree by
// coincidence, and one of them changing should not be able to reach the other.
func greenhouseTimestamp(raw string) time.Time {
	stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}
	}

	return stamp.UTC()
}

// Greenhouse returns all of the job postings for a given company, or an
// error if there was a problem making the request or parsing the response.
func Greenhouse(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		// The description body, departments and offices are one URL parameter
		// away, "?content=true" on this same request, but that parameter costs
		// about 13.7x the response size platform-wide and this is the largest
		// platform in the project; see the note on [greenhouseJobs]. It stays off
		// until a per-crawl option exists to ask for it.
		doc, err := fetchJSON[greenhouseJobs](ctx, httpClient, "Greenhouse", company, jsonRequest{
			URL: "https://boards-api.greenhouse.io/v1/boards/" + company + "/jobs",
		})
		if err != nil {
			yield(nil, err)

			return
		}

		for _, item := range doc.Jobs {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			url := strings.TrimSpace(strings.Replace(item.AbsoluteURL, "http://", "https://", -1))
			titleStr := strings.TrimSpace(item.Title)
			locationStr := strings.TrimSpace(item.Location.Name)

			if locationStr == "" {
				locationStr = "unknown/remote"
			}

			posting := &internal.JobPosting{
				Company:       company,
				URL:           url,
				Title:         titleStr,
				Location:      locationStr,
				PostedAt:      greenhouseTimestamp(item.FirstPublished),
				UpdatedAt:     greenhouseTimestamp(item.UpdatedAt),
				RequisitionID: strings.TrimSpace(string(item.RequisitionID)),
				Source: internal.PostingSource{
					Platform: greenhousePlatform,
					Key:      company,
				},
			}

			if item.ID > 0 {
				posting.ExternalID = strconv.FormatInt(item.ID, 10)
			}

			if !yield(posting, nil) {
				return
			}
		}
	}
}
