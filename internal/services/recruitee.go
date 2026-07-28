package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// recruiteePlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const recruiteePlatform = "recruitee"

func init() {
	registerBuiltin(recruiteePlatform, multiJobsFunc(Recruitee, RecruiteeCompanies))
}

// RecruiteeCompanies holds the Recruitee career sites this project crawls, one
// tenant subdomain per entry: "bunq" is https://bunq.recruitee.com.
//
// docs/source-backlog.md has wanted this platform for a while, and it is one of
// the cheapest lanes in the project: one keyless GET returns a tenant's entire
// open-req list with department, employment type, workplace flags, seniority,
// publish date and (for the tenants that opt in) an employer-published pay range
// already on it.
//
// # This list is measured, not staged
//
// All 507 slugs in testdata/candidates/recruitee_slugs.txt were probed live on
// 2026-07-28 at https://<slug>.recruitee.com/api/offers/, under the same shared
// limiter key internal/httpx gives *.recruitee.com:
//
//   - 489 answered HTTP 200 with a non-empty "offers" array. All are registered.
//   - 13 answered 200 with an empty array. docs/adding-a-source.md is explicit
//     that an empty board is not a broken one — but it is also not evidence
//     that a slug is live, so the ten that were not already registered stay in
//     the candidate file. The three that were already registered ("1x", "aihr",
//     "wallarm") are kept: a reachable board belonging to a company that is not
//     hiring today is a working source.
//   - 5 are dead, all HTTP 404: careermentors, luminis, agicap, and — this is
//     the part that mattered — "incentro" and "openclassrooms", both of which
//     were registered here and have been REMOVED. They were the platform's
//     entire failure count in a live health check, and neither was a bug in this
//     adapter; the boards are gone.
//
// The 489 live tenants published 9,832 postings between them at probe time,
// about 20 per HTTP request.
//
// Selection rules retained from when this list was written blind, and how the
// probe changed them: slugs whose employer identity is ambiguous were held back
// on the grounds that short generic slugs are first-come-first-served. That
// concern does not survive contact with the data, because nothing here asserts
// an identity — [internal.JobPosting.Company] is the tenant slug and every URL
// published is the board's own careers_url — so "make", "grid", "zara" and the
// rest are registered on the same evidence as everything else: they answered
// with postings. Recruiting agencies were also to be skipped where
// recognisable; that filter is not applied mechanically here either, since the
// live responses show them publishing their own internal reqs rather than
// republishing clients.
var RecruiteeCompanies = []string{
	"1x",
	"60secondstonapoli",
	"8advisory",
	"aaff",
	"academyofdigitalindustries",
	"ackermansvanhaaren",
	"adamsmithinternational1",
	"adhese",
	"adj",
	"admindagency",
	"adstronauts",
	"advancy",
	"aedifion",
	"affideagreece",
	"affideaireland",
	"affideaitaly",
	"affideaspain",
	"agaseurope",
	"aidigital",
	"aihr",
	"aikidosecurity",
	"alliade",
	"almavivadebelgique",
	"anandaventuresgmbh",
	"anchr",
	"apoint",
	"appetiser",
	"applus",
	"ascensionisland",
	"asksuite",
	"atasssports",
	"athora",
	"atostek",
	"auditdata",
	"autohausduennes",
	"auxmerveilleuxcarriere",
	"aveniq",
	"axi",
	"azubi",
	"baeckergoertz",
	"bakkergoedhartbroodspecialiteiten",
	"balchem",
	"ballastnedam",
	"ballysintralotsa",
	"bambuu",
	"barentskrans",
	"batitprotect",
	"battassocies",
	"bauersohnegmbhcokg",
	"bauhausnederlandcv",
	"baxmetaal",
	"beinguser",
	"benfatto",
	"biqh",
	"biscuitinternational",
	"biximontreal",
	"bloem",
	"bluecrux",
	"blueskygroup",
	"bluestonelogic",
	"boekenvoordeel",
	"bonial",
	"brainstack",
	"brenger",
	"buildofarm",
	"bundl",
	"bungemagdeburggmbh",
	"bunq",
	"burgbedrijven",
	"camelbackventures",
	"car24gmbh",
	"carbonbetter",
	"careexpert",
	"caretochange",
	"carlfriedrik",
	"cb",
	"cbsconsulting",
	"cce",
	"ccmeurope",
	"celebratecompany",
	"centreon",
	"centric",
	"cerbaresearch",
	"channable",
	"chgcareers",
	"chmedia",
	"chowco",
	"churchdesk",
	"cigusgmbh",
	"claranetitalia",
	"clario",
	"claus",
	"clay",
	"codaisseur",
	"comtest",
	"confirmo",
	"connectpeople",
	"consultdss",
	"contextand",
	"coppensmetaaltechniek",
	"cordesconsulting",
	"corygroup",
	"craftzing",
	"creativeclicks",
	"cronimetenvirotec",
	"crunchanalyticscareers",
	"cscfi",
	"ctscompositetechnologiesystemegmbh",
	"currentagruppe",
	"cvonlinelatvia",
	"dance",
	"dckgroup",
	"deleuropeamsterdam",
	"delonghigroup",
	"deluxeholidayhomes",
	"demoupcliplister",
	"denieuwezaak",
	"denjyskehaandvaerkerskole",
	"depiergroep",
	"diebayerische",
	"diegrenze",
	"digitalbeatgmbh",
	"dnata",
	"doccheckgroup",
	"dock",
	"doctenasa",
	"dommelvallei",
	"dpgmedia",
	"dreizen",
	"earlyalpineacademy",
	"ecconsultants",
	"ecolecentraledelyon",
	"econocomnederland",
	"elan",
	"elaphepropulsiontechnologies",
	"elektrosupersaxoag",
	"eleqtron",
	"elmleigh",
	"elockers",
	"eluscious",
	"ember",
	"emergentsoftware",
	"enecoemobility",
	"energyking",
	"eneve",
	"enseradesign",
	"enterprise",
	"entyre",
	"enviemholdingbv",
	"envipco",
	"epilot",
	"equalsmoney",
	"eskandary",
	"esportsworldcupfoundation",
	"eupagohub",
	"eurosparcareers",
	"evolit",
	"evreuxportesdenormandie",
	"exeon",
	"faactechnologies",
	"farogruppe",
	"fastems",
	"fastned",
	"festgmbh",
	"fidusheinenoord",
	"fieldbuddy",
	"floryn",
	"focusentertainment",
	"foleon",
	"forcyd",
	"formelio",
	"formofoodsgmbh",
	"forwardearth",
	"framestore",
	"frisbii",
	"funex",
	"gain",
	"gainpro",
	"gardec",
	"gemeentelansingerland",
	"gerardbertrand",
	"germanzeroev",
	"gieskerlaakmann",
	"gpvfinland",
	"gpvslovakia",
	"gpvswitzerland",
	"greatminds",
	"greenchoice",
	"greenflux",
	"greenpeacebelgium",
	"grid",
	"growprogress",
	"gtowizard",
	"halbersbacherhospitalitygroup",
	"hamelin",
	"hardrockdigital",
	"hascoinvest",
	"hauptstadtfloss",
	"heattransformers",
	"heembouw",
	"heidelbergmaterialsbenelux",
	"helloprint",
	"hemeriagroup",
	"highspiritshospitality",
	"hittechmultin",
	"holded",
	"holepunch",
	"hostaway",
	"hoteldrachten",
	"hotelgorinchem",
	"hotelmechelen",
	"hotelnazareth",
	"hoteltexel",
	"hotelzaltbommel",
	"hudsonmanpower",
	"huuuge",
	"hygraph",
	"ichoosr",
	"icuitservices",
	"igh",
	"iliabeauty",
	"inepro",
	"infomedijidoo",
	"inforsht",
	"innovamarketinsights",
	"innovativebeautygroup",
	"inretail",
	"institutminestelecom",
	"intent",
	"intergas",
	"internetwerving",
	"intersnacknederlandbv",
	"intralot",
	"iprofilegmbh",
	"jedowski",
	"jeffjill",
	"jobsatlanticvcfoodlabs",
	"jobsdeerns",
	"jobsdrlauterbachklinik",
	"jobse",
	"jobspeakcoaching",
	"jobster",
	"juliusbergerinternationalgmbh",
	"justrussel",
	"kaizo",
	"karriereboeckelsbestede",
	"keunecareers",
	"kiesertrainingag",
	"kodify",
	"koklinikgmbh",
	"koninklijkeniesternsander",
	"kpsnacks",
	"kravet",
	"kuoksingaporelimited",
	"layerscaleadvisory",
	"leaseabike",
	"lefebvresdu",
	"legalfly",
	"lemonwayjobcareers",
	"lenskartcareers",
	"lessonup",
	"leydenjar",
	"lfeddersenbaugesellschaftmbh",
	"lg2",
	"limeflight",
	"linkit",
	"lister",
	"livio",
	"lomography",
	"londiscareers",
	"loyaltykey",
	"lucidgames",
	"machinelearningreply",
	"madewithlove",
	"madlergmbh",
	"maierheiztechnikgmbh",
	"mainstream",
	"make",
	"makersitegmbh",
	"matera",
	"mayflower",
	"mbio",
	"mcdglobalhealth",
	"medmehealth",
	"meiarchitectsplanners",
	"merkur",
	"merkurosiguranjedd",
	"metrocaring",
	"mgid",
	"mobilexpense",
	"mobilityplus",
	"momentumdata",
	"momomedicalbv",
	"mondaymerch",
	"moneyhash",
	"monizze",
	"movares",
	"moveup",
	"mudwtr",
	"mumc",
	"myrunway",
	"n2jsoft",
	"nacardesign",
	"namecheap",
	"natuvion",
	"nemenergy",
	"neoday",
	"newlawbusinessmodel",
	"nicsystemhausgmbh",
	"nictiz",
	"nikin",
	"nmbrs",
	"novacard",
	"nts",
	"nvm",
	"nyrahealth",
	"o2h",
	"ockto",
	"odysseyhotelgroup",
	"oead",
	"oeadstudenthousing",
	"olsamgroup",
	"omniconsultcorp",
	"on2it",
	"onemobility",
	"onlinedialogue",
	"opergraz",
	"oscalabv",
	"pacmed",
	"palazzoversacedubai",
	"parent",
	"partejobs",
	"payconiq",
	"payflows",
	"peddler",
	"pelsrijcken",
	"peopleand",
	"petalmd",
	"picard",
	"pizzabeppe",
	"pladisfrance",
	"planeground",
	"polaroid",
	"poppy",
	"pragmaticcoders",
	"primeworks",
	"prinsenstichting",
	"printenbind",
	"properfood",
	"protectdemocracy",
	"qaamgo",
	"qualifyzegmbh",
	"radishlab",
	"railinnovatorsgroup",
	"rcube",
	"rebelmouse",
	"rebuy",
	"redsky",
	"riverflex",
	"rooysewissel",
	"royalswinkels",
	"royalwagenborg1",
	"rtl",
	"rvibefluides",
	"sahl",
	"salesup",
	"samy1",
	"santanagroup",
	"savantzorg",
	"secondhomejugendhilfegmbh",
	"sendcloud",
	"sequra",
	"shippingtechnology",
	"shopwareag",
	"shypple",
	"sidelineswap",
	"silverein",
	"skkarriere",
	"snapeda",
	"sovak3",
	"sparcareers",
	"sportakademiebaumann",
	"sportschuledefcon",
	"sspcs",
	"stacvalley",
	"steinemann",
	"stibbebv",
	"stichtingkwintes",
	"strata",
	"strictbv",
	"studentworks",
	"successday",
	"summit",
	"superlinear",
	"supremesportshospitality",
	"swisselect",
	"switchojob",
	"tacobellnederland",
	"talk360",
	"teamjobsleadprod",
	"techbizglobal",
	"technicaelectronics",
	"technicaengineeringgmbh",
	"testimonials",
	"tether",
	"tetrixtechniek",
	"theconfigteamcareers",
	"theentouragegroup",
	"thefactory",
	"thehospitalityrecruiters",
	"thehoteltaskforce",
	"thelandbankinggroup",
	"themortgagetalentnetwork",
	"theras",
	"thesafetynetwork",
	"thesalescentre",
	"thesupernicepeople",
	"thollembeek",
	"timedoctor",
	"timeoutstiftungggmbh",
	"tiugotech",
	"tkhomesolutions",
	"trustedshops",
	"twisto",
	"tytantechnologiesgmbh",
	"uhlenhaus",
	"unitedalsaqergroup",
	"up42gmbh",
	"utilityprofit",
	"uturn",
	"vacancies",
	"vanbeestbv",
	"vandebron",
	"vandervalkamsterdam",
	"vandervalkhotelvenlo",
	"vandervalkluxembourg",
	"vbtgroep",
	"veaxo",
	"vebegofacilityservices3",
	"vebegolandscapingservices",
	"vencomaticgroup",
	"veocareers",
	"veratronag",
	"verduurzaamvastgoed1",
	"vertigis",
	"vertuoza",
	"vesper",
	"vicentra",
	"vilgain",
	"vionfoodgroup",
	"viviq",
	"vlyfoods",
	"voltaira",
	"vpteam",
	"wacomeurope",
	"wagenborgnedlift",
	"wagenborgpassagiersdiensten",
	"wakuli",
	"walibiholland",
	"wallarm",
	"warande",
	"webbtraders",
	"wellsterhealthtechgroup",
	"werkenbijdeafm",
	"werkenbijdecantharel",
	"werkenbijflagshipamsterdam",
	"werkenbijhiltermann",
	"werkenbijnlr",
	"werkenbijons",
	"werkenbijpsyned",
	"werkenbijstichtingmeerscholen",
	"werkenbijvigogroep",
	"werkenbijvnoncwmkb",
	"westerhorstmann",
	"whello",
	"wizdaa",
	"wjgruppe",
	"wmreplyjobs",
	"woonplusschiedam",
	"woonzorgnederland",
	"wtt",
	"xalution",
	"xebiapoland",
	"xfive",
	"xite",
	"yellowtail",
	"zara",
	"zelh",
	"zensai",
	"zonnehuisgroepamstelland",
	"zpvgmbhcokg",
	"zteam",
}

// recruiteeOffersResponse is one tenant's whole open-req list.
//
// Offers is a pointer to a slice on purpose. An absent "offers" key and an empty
// one decode identically into a plain []recruiteeOffer, and a source that
// reports zero postings because the response stopped being the response this
// adapter parses is the silently-empty failure docs/architecture-roadmap.md
// treats as the worst one available. A nil here means the key was missing, which
// is a shape change and an error; a non-nil empty slice means the tenant is not
// hiring today, which docs/adding-a-source.md is explicit is not a failure.
type recruiteeOffersResponse struct {
	Offers *[]recruiteeOffer `json:"offers"`
}

// recruiteeOffer is one opening on a Recruitee career site.
//
// Only the fields this adapter actually publishes are modelled, per
// docs/adding-a-source.md: the same response also carries the full HTML
// description, requirements, tags and an applicant-form schema, none of which
// [internal.JobPosting] has anywhere to put.
type recruiteeOffer struct {
	// ID is Recruitee's own numeric posting id. It outlives the URL, which a
	// re-titled posting changes, and URL-keyed [internal.Dedupe] cannot follow
	// that.
	ID recruiteeScalar `json:"id"`

	// Slug is the posting's path segment, used to rebuild the public URL when
	// careers_url is absent.
	Slug  string `json:"slug"`
	Title string `json:"title"`

	// CareersURL is the public posting page on the tenant's career site.
	CareersURL string `json:"careers_url"`

	Department string `json:"department"`

	// EmploymentTypeCode is Recruitee's own vocabulary, normalized rather than
	// stored raw.
	//
	// docs/research/ats-platform-survey.md describes it as plain "fulltime" /
	// "parttime" / "internship" / "freelance" / "temporary". The live vocabulary
	// measured across 9,832 postings on 2026-07-28 is mostly COMPOUND, tenure
	// glued onto hours: "fulltime_permanent" (5,196), "fulltime_fixed_term"
	// (1,602), "parttime_fixed_term" (999), "parttime_permanent" (574),
	// "contract" (570), "internship" (293), "freelance" (147), bare "fulltime"
	// (138), "parttime_minijob" (118) and "temporary" (64).
	//
	// [internal.NormalizeEmploymentType] reads all of them correctly, because it
	// squashes separators away and then matches "fulltime" and "parttime" as
	// substrings before it reaches "temporary" — so "fulltime_fixed_term" lands
	// on full-time rather than temporary. Nothing needs to change, but an
	// adapter that had built an exact-match table from the survey's five values
	// would have labelled 88% of this platform unknown.
	EmploymentTypeCode string `json:"employment_type_code"`

	// ExperienceCode is Recruitee's seniority vocabulary. The survey does not
	// document it and the reference implementation this adapter was written
	// against does not read it, so it was decoded here on speculation. The probe
	// settles it: the key is present and populated on all 9,832 postings
	// measured, spelling levels as "mid_level" (3,401), "experienced" (3,256),
	// "entry_level" (2,114), "student_school", "student_college", "manager",
	// "senior_manager" and "senior_executive".
	ExperienceCode string `json:"experience_code"`

	// Remote, Hybrid and OnSite are Recruitee's workplace flags.
	//
	// The survey documents only remote(bool), which is why this adapter
	// originally mapped everything that was not remote to
	// [internal.WorkplaceTypeUnknown]. All three keys are in fact present on all
	// 9,832 postings measured on 2026-07-28, and reading only the first one
	// discarded a real structured answer for 90% of the platform: exactly one
	// flag is set on 8,841 postings — on_site 5,876, hybrid 2,057, remote 908 —
	// and the remaining 991 set more than one.
	//
	// They are pointers so a tenant that stops sending hybrid/on_site degrades to
	// the remote-only reading rather than being read as "not hybrid", see
	// [recruiteeWorkplaceType].
	Remote *bool `json:"remote"`
	Hybrid *bool `json:"hybrid"`
	OnSite *bool `json:"on_site"`

	City     string `json:"city"`
	Country  string `json:"country"`
	Location string `json:"location"`

	// PublishedAt is when the posting went live, formatted like
	// "2026-05-28 20:36:05 UTC" rather than as ISO-8601.
	PublishedAt string `json:"published_at"`

	// UpdatedAt is decoded opportunistically and its name is likewise unverified
	// here. It is deliberately never used to stand in for PublishedAt: editing a
	// description does not make a nine-month-old req new, and
	// [internal.Filter.PostedSince] would then quietly fill a "posted this week"
	// query with stale postings.
	UpdatedAt string `json:"updated_at"`

	// Salary is the employer-published pay range, present only for the tenants
	// that opt into showing pay.
	Salary struct {
		Min      recruiteeScalar `json:"min"`
		Max      recruiteeScalar `json:"max"`
		Currency string          `json:"currency"`

		// Period is Recruitee's own interval spelling ("month", "year"). Its
		// field name is unverified here; when it is absent
		// [internal.Compensation] infers the period from the magnitude of the
		// figures, which is the documented fallback.
		Period string `json:"period"`
	} `json:"salary"`
}

// recruiteeScalar decodes a JSON value whose type Recruitee does not hold stable
// into a string.
//
// The reference implementation this adapter was written against reads the
// posting id straight into a string and guards each salary bound with an
// explicit numeric type check before using it, which is what a field that
// arrives sometimes as a number and sometimes as a string looks like from the
// outside. Modelling either as a Go float64 would make one such tenant fail to
// decode, and fetchJSON decodes the whole response at once, so a single odd
// salary would take down every posting that company has. That is the same
// failure greenhouseScalar exists to prevent on Greenhouse's requisition_id.
type recruiteeScalar string

// UnmarshalJSON implements [json.Unmarshaler].
func (s *recruiteeScalar) UnmarshalJSON(data []byte) error {
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

		*s = recruiteeScalar(strings.TrimSpace(text))

		return nil
	}

	// An object or an array is neither an id nor an amount, and rendering its
	// literal JSON into the field would publish "{...}" as a salary.
	if trimmed[0] == '{' || trimmed[0] == '[' {
		*s = ""

		return nil
	}

	*s = recruiteeScalar(trimmed)

	return nil
}

// amount reads the scalar as a pay figure, reporting false when it is not one.
func (s recruiteeScalar) amount() (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64)
	if err != nil || value <= 0 {
		return 0, false
	}

	return value, true
}

// recruiteePeriods maps Recruitee's interval spelling onto [internal.Period].
// Both the bare unit and the adverbial form are accepted because the field could
// not be probed and boards spell this either way.
var recruiteePeriods = map[string]internal.Period{
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

// recruiteePeriod parses Recruitee's salary interval, returning
// [internal.PeriodUnknown] for anything unrecognised so that
// [internal.Compensation] falls back to inferring the period from magnitude
// rather than this adapter guessing one.
func recruiteePeriod(raw string) internal.Period {
	return recruiteePeriods[strings.ToLower(strings.TrimSpace(raw))]
}

// recruiteeTimeLayouts are the shapes a Recruitee timestamp has been described
// as arriving in, most specific first.
//
// The documented one is "2026-05-28 20:36:05 UTC", which is neither RFC 3339 nor
// anything [time.Parse] recognises without being told. RFC 3339 is tried anyway
// because it costs nothing and a board that modernises its format should not
// silently lose its dates.
var recruiteeTimeLayouts = []string{
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05",
	time.RFC3339,
	"2006-01-02",
}

// recruiteeTime parses one of Recruitee's timestamps into UTC, returning the
// zero time when the board published none or the value cannot be read.
//
// An unreadable value yields the zero time rather than an error: one posting
// with an odd timestamp must not cost a board its other postings, and
// [internal.Filter.PostedSince] excludes undated postings anyway, so the failure
// mode is a posting missing from a date query rather than a wrong date in it.
func recruiteeTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}

	for _, layout := range recruiteeTimeLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			// Stored in UTC so comparing a Recruitee posting with an Ashby one is
			// a comparison of instants rather than of the zones two boards
			// happened to render in.
			return parsed.UTC()
		}
	}

	return time.Time{}
}

// recruiteeLocation renders the place a posting is offered at.
//
// Recruitee publishes both a prebuilt "location" string and the city/country
// parts it was built from. The prebuilt one is preferred because it is what the
// employer sees on their own careers page; the parts are the fallback for the
// tenants that leave it empty.
func recruiteeLocation(offer recruiteeOffer) string {
	if location := strings.TrimSpace(offer.Location); location != "" {
		return location
	}

	parts := make([]string, 0, 2)

	for _, part := range []string{offer.City, offer.Country} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}

	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}

	if offer.Remote != nil && *offer.Remote {
		return "Remote"
	}

	return "unknown"
}

// recruiteeCompensation turns Recruitee's salary object into a pay range,
// returning nil when the tenant published none.
//
// Provenance is [internal.ProvenanceEmployer]: these are dedicated numeric
// fields the employer filled in, not figures read out of prose, and
// docs/compensation.md requires the two never be blended.
func recruiteeCompensation(offer recruiteeOffer) *internal.Compensation {
	comp := &internal.Compensation{
		Currency:   strings.ToUpper(strings.TrimSpace(offer.Salary.Currency)),
		Period:     recruiteePeriod(offer.Salary.Period),
		Provenance: internal.ProvenanceEmployer,
	}

	if minimum, ok := offer.Salary.Min.amount(); ok {
		comp.Min = minimum
	}

	if maximum, ok := offer.Salary.Max.amount(); ok {
		comp.Max = maximum
	}

	// A currency with no figures is not a pay range; publishing it would make
	// --has-pay match postings that disclose nothing.
	if comp.IsZero() {
		return nil
	}

	return comp
}

// recruiteeOfferURL returns the public posting page.
//
// careers_url is what the board publishes, and the "/o/<slug>" form is the same
// URL rebuilt from parts for the tenants that omit it. A posting with neither is
// dropped: this project's output is a list of links, and a posting without one
// is not a lead.
func recruiteeOfferURL(company string, offer recruiteeOffer) string {
	if url := strings.TrimSpace(offer.CareersURL); strings.HasPrefix(url, "https://") {
		return url
	}

	if slug := strings.TrimSpace(offer.Slug); slug != "" {
		return "https://" + company + ".recruitee.com/o/" + slug
	}

	return ""
}

// Recruitee returns all of the job postings for one Recruitee career site, or an
// error if there was a problem making the request or parsing the response.
//
// company is the tenant's subdomain, see [RecruiteeCompanies].
//
// There is no pagination here, deliberately: /api/offers/ answers with the
// tenant's entire open-req list in one response, so there is no page parameter
// for a board to ignore and no loop for [pageRepeatGuard] to bound. The
// equivalent hazard for a single-shot endpoint is a response that decodes
// cleanly into nothing, which is what the two shape checks below are for.
func Recruitee(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	// https://$company.recruitee.com/
	// https://$company.recruitee.com/api/offers/
	// https://$company.recruitee.com/o/$offer_slug
	return func(yield func(*internal.JobPosting, error) bool) {
		offersURL := "https://" + company + ".recruitee.com/api/offers/"

		doc, err := fetchJSON[recruiteeOffersResponse](ctx, httpClient, "Recruitee", company, jsonRequest{URL: offersURL})
		if err != nil {
			yield(nil, err)

			return
		}

		if doc.Offers == nil {
			yield(nil, fmt.Errorf("unexpected response shape from Recruitee for company %q at %s: no %q key, so this is not the offers feed this adapter reads", company, offersURL, "offers"))

			return
		}

		offers := *doc.Offers
		yielded := 0

		for _, offer := range offers {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			title := strings.TrimSpace(offer.Title)
			url := recruiteeOfferURL(company, offer)

			if title == "" || url == "" {
				continue
			}

			posting := &internal.JobPosting{
				Company:  company,
				URL:      url,
				Title:    title,
				Location: recruiteeLocation(offer),

				Compensation:  recruiteeCompensation(offer),
				Department:    strings.TrimSpace(offer.Department),
				WorkplaceType: recruiteeWorkplaceType(offer),
				Seniority:     strings.TrimSpace(offer.ExperienceCode),
				PostedAt:      recruiteeTime(offer.PublishedAt),
				UpdatedAt:     recruiteeTime(offer.UpdatedAt),
				ExternalID:    strings.TrimSpace(string(offer.ID)),
				Source: internal.PostingSource{
					Platform: recruiteePlatform,
					Key:      company,
				},
			}

			// Remote is carried whenever the board named a single arrangement,
			// and only then.
			//
			// The reasoning workable.go sets out — that remote=false says "not
			// marked remote" and nothing more, so storing it would switch off
			// the location-text fallback in [internal.JobPosting.IsRemote] —
			// applies to a board with one flag. Recruitee publishes three, so
			// hybrid alone or on_site alone is the employer positively stating
			// the role is not fully remote, which is a fact and not an absence.
			// That is the same argument pinpoint.go makes for its three-state
			// field. Where two or more flags are set the board named no single
			// arrangement, so this stays nil and the heuristic keeps deciding.
			switch posting.WorkplaceType {
			case internal.WorkplaceTypeRemote:
				remote := true

				posting.Remote = &remote
			case internal.WorkplaceTypeHybrid, internal.WorkplaceTypeOnsite:
				remote := false

				posting.Remote = &remote
			}

			// An unrecognised spelling leaves the field empty rather than
			// guessing: a wrong employment type cannot be told apart from a right
			// one by a filter, while an absent one is visibly absent.
			if employment, ok := internal.NormalizeEmploymentType(offer.EmploymentTypeCode); ok {
				posting.EmploymentType = employment
			}

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// A response full of offers that produced no postings at all means every
		// one of them was missing a title or a URL, which no live board does. It
		// is the signature of a renamed field, and reporting zero postings for it
		// would be indistinguishable from a company that is not hiring.
		if len(offers) > 0 && yielded == 0 {
			yield(nil, fmt.Errorf("unexpected response shape from Recruitee for company %q at %s: %d offers decoded but none carried both a title and a URL", company, offersURL, len(offers)))
		}
	}
}

// recruiteeWorkplaceType resolves where the work happens from the three
// independent booleans Recruitee publishes.
//
// They are independent, not a three-state enum, and the live data uses that: of
// 9,832 postings measured on 2026-07-28, 8,841 set exactly one flag and 991 set
// two or three — an employer offering a role on more than one arrangement.
//
// So exactly one flag set is the board naming an arrangement, and that is the
// answer. More than one is the board declining to name one, and this returns
// unknown rather than picking the flag it likes best; a wrong workplace type
// cannot be told apart from a right one by [internal.Filter], while an absent
// one is visibly absent. None set does not occur live, and also returns unknown.
//
// A tenant that does not send hybrid and on_site at all falls back to the
// remote-only reading this adapter shipped with, which is why the flags are
// pointers: a missing key must not be read as a false one.
func recruiteeWorkplaceType(offer recruiteeOffer) internal.WorkplaceType {
	if offer.Hybrid == nil && offer.OnSite == nil {
		if offer.Remote != nil && *offer.Remote {
			return internal.WorkplaceTypeRemote
		}

		return internal.WorkplaceTypeUnknown
	}

	var (
		set     int
		matched internal.WorkplaceType
	)

	for _, flag := range []struct {
		value *bool
		typ   internal.WorkplaceType
	}{
		{offer.Remote, internal.WorkplaceTypeRemote},
		{offer.Hybrid, internal.WorkplaceTypeHybrid},
		{offer.OnSite, internal.WorkplaceTypeOnsite},
	} {
		if flag.value != nil && *flag.value {
			set++
			matched = flag.typ
		}
	}

	if set != 1 {
		return internal.WorkplaceTypeUnknown
	}

	return matched
}
