package services

import (
	"context"
	"net/http"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin(multiJobsFunc(AshbyHQ, AshbyHQCompanies))
}

var AshbyHQCompanies = []string{
	"0x",
	"1password",
	"abridge",
	"adaptivesecurity",
	"addi",
	"adonis",
	"airapps",
	"airbyte",
	"airgarage",
	"airspace-intelligence.com",
	"airwallex",
	"alan",
	"alchemy",
	"allinbits",
	"almedia",
	"altura",
	"ambiencehealthcare",
	"amo",
	"amplitude",
	"andela",
	"anima",
	"anterior",
	"anyscale",
	"arcade",
	"ashby",
	"assembledhq",
	"assorthealth",
	"assured",
	"astral",
	"astronomer",
	"attio",
	"aurorasolar",
	"authzed",
	"aven",
	"avid4",
	"axiom",
	"axiom-co",
	"babbel",
	"baseten",
	"beavr",
	"ben",
	"bifrost",
	"blacksmith",
	"bland",
	"blockdaemon",
	"blockworks",
	"braintrust",
	"brigit",
	"browserbase",
	"cambly",
	"candidhealth",
	"capchase",
	"cape",
	"capimoney",
	"cargado",
	"cargo-one",
	"cartesia",
	"casca",
	"catio",
	"cedar",
	"cerebras",
	"chainalysis-careers",
	"chapter",
	"character",
	"checkly",
	"chestnut",
	"circle",
	"clarisights",
	"claylabs",
	"clearco",
	"clerk",
	"clickhouse",
	"clickup",
	"close",
	"cloudzero",
	"coder",
	"coderabbit",
	"cognition",
	"cohere",
	"cointracker",
	"color-health",
	"column",
	"commure",
	"compa",
	"conception",
	"conduit",
	"confluent",
	"convex-dev",
	"corti",
	"cosine",
	"counsel",
	"count",
	"coursecareers",
	"credal",
	"crusoe",
	"cruxclimate",
	"cryptio",
	"cursor",
	"cyberhaven",
	"d-matrix",
	"dandy",
	"dapper",
	"dave",
	"decagon",
	"deel",
	"deepgram",
	"deepnote",
	"delinea",
	"deliveroo",
	"depot",
	"diagrid",
	"docker",
	"doppel",
	"doppler",
	"drata",
	"dune",
	"e2b",
	"eightsleep",
	"elevenlabs",
	"eli",
	"elliptic",
	"eon systems",
	"etched",
	"eventualcomputing",
	"exa",
	"extend",
	"eye-security",
	"factory",
	"fathom.video",
	"finch",
	"fireworksai",
	"flatfile",
	"flock",
	"forma",
	"formenergy",
	"frontcareers",
	"fullstory",
	"g2i",
	"gamechanger",
	"genomics",
	"gitbook",
	"gorgias",
	"goteleport",
	"gptzero",
	"graphite",
	"greptile",
	"griffin",
	"gruntwork",
	"hackerone",
	"handshake",
	"harvey",
	"hawkeyeinnovations",
	"headway",
	"healthaxis",
	"helius",
	"hellobrightline",
	"helpscout",
	"highbeam",
	"hiive",
	"hims-and-hers",
	"hiya",
	"homebase",
	"hopper",
	"horizon3ai",
	"humaans",
	"hyperbolic",
	"hyperexponential",
	"ideogram",
	"illumio",
	"immersivelabs",
	"imprint",
	"incident",
	"infisical",
	"influxdata",
	"inngest",
	"january",
	"julius",
	"jump",
	"junipersquare",
	"keep",
	"keyrock",
	"kin",
	"kindred",
	"kong",
	"kraken.com",
	"kustomer",
	"lambda",
	"langchain",
	"langfuse",
	"lavendo",
	"leapsome",
	"ledger",
	"lemonade",
	"levelpath",
	"levels",
	"lightdash",
	"lightning",
	"lightspeed",
	"linear",
	"livekit",
	"llamaindex",
	"lovable",
	"luxor",
	"mach",
	"madhive",
	"magiceden",
	"magicpatterns",
	"mapbox",
	"marshmallow",
	"materialize",
	"materialsecurity",
	"mazedesign",
	"mend-io",
	"menlosecurity",
	"metriport",
	"middesk",
	"midjourney",
	"mintlify",
	"miro",
	"mobbin.com",
	"modal",
	"modernfi",
	"moderntreasury",
	"mollie",
	"monad.foundation",
	"monarchmoney",
	"mondoo",
	"montecarlodata",
	"mosey",
	"motherduck",
	"multiverse",
	"mural",
	"n8n",
	"nabla",
	"nango",
	"neon",
	"nerdwallet",
	"netboxlabs",
	"netgear",
	"newfront",
	"nivoda",
	"nooks",
	"northflank.com",
	"northwoodspace",
	"notable",
	"notion",
	"novo",
	"nucleus",
	"nudgesecurity",
	"omni",
	"oneapp",
	"onebrief",
	"oneleet",
	"opal",
	"openai",
	"opengov",
	"openly",
	"opensea",
	"oplabs",
	"opslevel",
	"optimum",
	"orb",
	"oso",
	"outschool",
	"oyster",
	"padlet",
	"paradigm",
	"parafin",
	"paragon",
	"Passthrough",
	"patreon",
	"paxos",
	"peek",
	"percona",
	"perfect-venue",
	"permitflow",
	"perplexity",
	"persona",
	"phantom",
	"pika",
	"pinecone",
	"plaid",
	"pluralis-research",
	"poolside",
	"posh",
	"post",
	"posthog",
	"prefect",
	"prior-labs",
	"probook",
	"prompt",
	"pylon",
	"qonto",
	"quora",
	"quotewell",
	"radai",
	"radar",
	"railway",
	"rain",
	"ramp",
	"range",
	"rasa",
	"re-cap",
	"reach",
	"readme",
	"ready",
	"real",
	"redis",
	"reka",
	"render",
	"replit",
	"replo",
	"resend",
	"resolve ai",
	"restate",
	"rho",
	"river",
	"rogo",
	"rula",
	"runpod",
	"runway",
	"rutter",
	"safe",
	"sahara",
	"salient",
	"sanity",
	"sardine",
	"sciencelogic",
	"seconddinner",
	"secureframe",
	"semgrep",
	"semperis",
	"sentra",
	"sentry",
	"sequoia",
	"seriesai",
	"sfcompute",
	"shiftkey",
	"sierra",
	"sift",
	"signoz",
	"skyflow",
	"skymavis",
	"slope",
	"smallstep",
	"snowflake",
	"socket",
	"socure",
	"solace",
	"solanalabs",
	"speak",
	"speakeasy",
	"statisfy",
	"statista",
	"staycation",
	"stedi",
	"steel",
	"stream",
	"stytch",
	"succinct",
	"suno",
	"supabase",
	"sweep",
	"synctera",
	"synthesia",
	"synthflow",
	"tabs",
	"tacto",
	"taktile",
	"talos-trading",
	"tavily",
	"temporal",
	"tennr",
	"the browser company",
	"theydo",
	"tigerdata",
	"tracebit",
	"trainline",
	"triggerdev",
	"truemed",
	"trychroma",
	"turbopuffer",
	"turnkey",
	"uipath",
	"unify",
	"union-tech",
	"uniswap",
	"unit",
	"upflow",
	"upguard",
	"upside",
	"upvest",
	"vanta",
	"vantage",
	"vapi",
	"vastai",
	"vellum",
	"vetcove",
	"virtahealth",
	"vultr",
	"warp",
	"watershed",
	"wayflyer",
	"weaviate",
	"welltech",
	"windmill",
	"witnessai",
	"workos",
	"worldly",
	"wrapbook",
	"writer",
	"wundergraph",
	"xbowcareers",
	"xero",
	"zapier",
	"zello",
	"zip",
	"zip security",
}

type ashbyHQJobs struct {
	Jobs []struct {
		URL      string `json:"jobUrl"`
		Title    string `json:"title"`
		Location string `json:"location"`

		// Ashby publishes a real structured remote flag, unlike most boards.
		IsRemote bool `json:"isRemote"`

		// Compensation is only present when the request asks for it and the
		// company has opted into showing pay. Measured at 268 of 349 postings for
		// one company, and zero for several others.
		Compensation struct {
			Summary string `json:"compensationTierSummary"`

			Tiers []struct {
				Components []struct {
					// CompensationType distinguishes base salary from equity and
					// commission. Only salary carries a comparable range.
					CompensationType string   `json:"compensationType"`
					Interval         string   `json:"interval"`
					CurrencyCode     *string  `json:"currencyCode"`
					MinValue         *float64 `json:"minValue"`
					MaxValue         *float64 `json:"maxValue"`
				} `json:"components"`
			} `json:"compensationTiers"`
		} `json:"compensation"`
	} `json:"jobs"`
}

// ashbyIntervals maps Ashby's compensation interval onto [internal.Period].
var ashbyIntervals = map[string]internal.Period{
	"YEAR":  internal.PeriodYear,
	"MONTH": internal.PeriodMonth,
	"WEEK":  internal.PeriodWeek,
	"DAY":   internal.PeriodDay,
	"HOUR":  internal.PeriodHour,
}

// ashbyPeriod parses Ashby's interval string into a pay period.
//
// The values are quantified rather than bare enums, "1 YEAR", not "YEAR", and
// equity components use "NONE". The count is dropped and the unit pluralization
// normalized, so "2 WEEKS" is read as a weekly rate.
func ashbyPeriod(interval string) internal.Period {
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(interval)))
	if len(fields) == 0 {
		return internal.PeriodUnknown
	}

	unit := strings.TrimSuffix(fields[len(fields)-1], "S")

	return ashbyIntervals[unit]
}

func AshbyHQ(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		// includeCompensation is required to get the pay range at all: without
		// it the compensation key is absent from the response entirely, not
		// merely empty. It is undocumented in the plain endpoint.
		baseURL := "https://api.ashbyhq.com/posting-api/job-board/" + company + "?includeCompensation=true"

		jobs, err := fetchJSON[ashbyHQJobs](ctx, httpClient, "AshbyHQ", company, jsonRequest{URL: baseURL})
		if err != nil {
			yield(nil, err)

			return
		}

		for _, job := range jobs.Jobs {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}
			remote := job.IsRemote

			if !yield(&internal.JobPosting{
				Company:      company,
				URL:          job.URL,
				Title:        job.Title,
				Location:     job.Location,
				Remote:       &remote,
				Compensation: ashbyCompensation(job.Compensation.Summary, job.Compensation.Tiers),
			}, nil) {
				return
			}
		}
	}
}

// ashbyCompensation extracts the base-salary range from Ashby's compensation
// tiers, returning nil when the company publishes none.
//
// Only the Salary component is turned into a range: the same tier also carries
// equity and commission components whose values are not comparable with a salary,
// and which frequently have null amounts. The board's own summary string is kept
// regardless, since it captures those extras in a form worth showing.
func ashbyCompensation(summary string, tiers []struct {
	Components []struct {
		CompensationType string   `json:"compensationType"`
		Interval         string   `json:"interval"`
		CurrencyCode     *string  `json:"currencyCode"`
		MinValue         *float64 `json:"minValue"`
		MaxValue         *float64 `json:"maxValue"`
	} `json:"components"`
}) *internal.Compensation {
	comp := &internal.Compensation{
		Summary:    strings.TrimSpace(summary),
		Provenance: internal.ProvenanceEmployer,
	}

	for _, tier := range tiers {
		for _, component := range tier.Components {
			if !strings.EqualFold(component.CompensationType, "Salary") {
				continue
			}

			if component.MinValue != nil {
				comp.Min = *component.MinValue
			}

			if component.MaxValue != nil {
				comp.Max = *component.MaxValue
			}

			if component.CurrencyCode != nil {
				comp.Currency = *component.CurrencyCode
			}

			comp.Period = ashbyPeriod(component.Interval)

			if comp.Min > 0 || comp.Max > 0 {
				return comp
			}
		}
	}

	if comp.IsZero() {
		return nil
	}

	return comp
}
