package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobpostings"
	"github.com/spf13/cobra"
)

// ls *.go | grep -v 'helper' | grep -v '_test' | awk -F '.' '{print "\""$1"\""","}'
var sourcesList = []string{
	"0x",
	"21st_century_fox",
	"3m",
	"adjust",
	"adobe",
	"aes",
	"air_map",
	"airbnb",
	"airtame",
	"alterian",
	"alto",
	"amazee",
	"anchorage",
	"apptio",
	"azerion",
	"beamly",
	"better_lesson",
	"better_up",
	"bishop_fox",
	"bittrex",
	"blackfynn",
	"bombas",
	"bose",
	"braintree",
	"bright_bytes",
	"britecore",
	"buildium",
	"buzzfeed",
	"cae",
	"canonical",
	"carbon_black",
	"carousell",
	"casper",
	"catalytic",
	"cb_insights",
	"cheddar",
	"chewy",
	"chimp",
	"cisco_meraki",
	"city_and_county_of_denver",
	"cloud_bees",
	"cloudflare",
	"cloudreach",
	"cockroach_labs",
	"codecademy",
	"coinbase",
	"colossal",
	"compass",
	"conductor_technologies",
	"conrell_university",
	"core_scientific",
	"corelight",
	"curalate",
	"datica",
	"datto",
	"dell",
	"digital_ocean",
	"dnb",
	"docker",
	"dots",
	"drop",
	"duo_security",
	"early_warning",
	"elastic",
	"epic_games",
	"erm",
	"evercommerce",
	"evernote",
	"expedia",
	"expensify",
	"farmers_buisness_network",
	"fastly",
	"fetch_rev",
	"fico",
	"finger_food_studios",
	"first",
	"game_changer",
	"gates_foundation",
	"get_all",
	"github",
	"gitlab",
	"gizmodo",
	"glow",
	"go_daddy",
	"gov_predict",
	"gradient",
	"gradle",
	"grammarly",
	"greenpeace",
	"gridspace",
	"gusto",
	"hashicorp",
	"hello_alfred",
	"hello_fresh",
	"hifyre",
	"hopsy",
	"imageworks",
	"input",
	"instabase",
	"instacart",
	"inventables",
	"invitae",
	"iota",
	"jda",
	"jet",
	"job_posting",
	"joor",
	"journera",
	"kantar",
	"kayak",
	"kik",
	"kinnek",
	"light_step",
	"lighthouse_studios",
	"limeade",
	"live_nation",
	"logitech",
	"lyft",
	"lyric",
	"magic_leap",
	"major_league_baseball",
	"mapbox",
	"mastercard",
	"maven_clinic",
	"mavens",
	"measurabl",
	"message_bird",
	"metal_toad",
	"mighty_networks",
	"modernize",
	"modsy",
	"mongodb",
	"movable_ink",
	"narvar",
	"nationwide",
	"netlify",
	"new_engen",
	"new_york_times",
	"nexient",
	"nextdoor",
	"ns1",
	"nvidia",
	"okta",
	"omada_health",
	"omaze",
	"one_login",
	"open_cosmos",
	"open_fin",
	"pae",
	"palantir",
	"palo_alto_networks",
	"path_ai",
	"pathlight",
	"patreon",
	"pax",
	"pfizer",
	"pinterest",
	"pioneer_square_labs",
	"platformsh",
	"policygenius",
	"postmates",
	"predictive_index",
	"procore",
	"psi_kick",
	"pushpay",
	"qualtrics",
	"qualys",
	"rapid7",
	"reddit",
	"relayr",
	"remarkably",
	"remitly",
	"riot_games",
	"robinhood",
	"roll_pay",
	"rolls_royce",
	"rubrik",
	"salesforce",
	"salt_stack",
	"samsara",
	"samsung",
	"sauce_labs",
	"scandit",
	"screen_cloud",
	"security_trails",
	"securonix",
	"sentry",
	"shape_scale",
	"shoes",
	"sift",
	"simpli_safe",
	"smartsheet",
	"snap_raise",
	"spaceflight_industries",
	"spacex",
	"spot_hero",
	"spotify",
	"sprout_social",
	"squarespace",
	"state_steet",
	"stauer",
	"strava",
	"stripe",
	"sumo_logic",
	"survey_gizmo",
	"survey_monkey",
	"swat",
	"symantec",
	"sysdig",
	"t1cg",
	"tableau",
	"tablexi",
	"taco_bell",
	"tala",
	"telaria",
	"the_trade_desk",
	"tilting_point",
	"time_inc",
	"toptal",
	"trilogy_education",
	"trip_advisor",
	"ttt_studios",
	"tutela",
	"twilio",
	"uber",
	"uberether",
	"udacity",
	"uniregistry",
	"unisys",
	"unity3d",
	"upstart",
	"venafi",
	"venmo",
	"verifone",
	"verishop",
	"veritas",
	"verizon_media",
	"vewd",
	"vice",
	"vox_media",
	"voxter",
	"web3",
	"wgames",
	"whisper",
	"whole_foods",
	"wizeline",
	"xylem",
	"zenefits",
	"zero_cater",
	"zerofox",
	"zipwhip",
	"zume",
	"zyris",
}

func main() {
	cleanup := func() {
		os.Exit(0)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			cleanup()
		}
	}()

	var (
		cmdJobPostingsPrintJSON bool
		cmdJobPostingsPrintCSV  bool
	)

	var cmdJobPostings = &cobra.Command{
		Use:   "job-postings [flags]",
		Short: "Find job postings from various companies",
		Run: func(cmd *cobra.Command, args []string) {
			var printer func(j *jobpostings.JobPosting)

			if cmdJobPostingsPrintJSON {
				printer = func(j *jobpostings.JobPosting) {
					data, err := json.Marshal(j)
					if err == nil {
						fmt.Println(string(data))
					}
				}
			} else if cmdJobPostingsPrintCSV {
				printerWrapper := func() func(j *jobpostings.JobPosting) {
					w := csv.NewWriter(os.Stdout)

					return func(j *jobpostings.JobPosting) {
						record := []string{j.Title, j.Location, j.URL}
						if err := w.Write(record); err != nil {
							panic(err)
						}
					}
				}
				printer = printerWrapper()
			} else {
				printer = func(j *jobpostings.JobPosting) {
					fmt.Println("title:", j.Title, "location:", j.Location, "url:", j.URL)
				}
			}

			for jobPosting := range jobpostings.GetAllJobPostings(context.Background()) {
				printer(jobPosting)
			}
		},
	}
	cmdJobPostings.Flags().BoolVar(&cmdJobPostingsPrintJSON, "json", false, "output as newline separated JSON")
	cmdJobPostings.Flags().BoolVar(&cmdJobPostingsPrintCSV, "csv", false, "output as CSV with no header (title, location, url)")

	var (
		cmdJobSourcesPrintJSON bool
		cmdJobSourcesPrintCSV  bool
	)

	var cmdJobSources = &cobra.Command{
		Use:   "job-sources [flags]",
		Short: "List the various companies available from job-postings",
		Run: func(cmd *cobra.Command, args []string) {
			var printer func(s []string)

			if cmdJobSourcesPrintJSON {
				printer = func(s []string) {
					data, err := json.Marshal(s)
					if err == nil {
						fmt.Println(string(data))
					}
				}
			} else if cmdJobSourcesPrintCSV {
				printer = func(s []string) {
					fmt.Println(strings.Join(s, ","))
				}
			} else {
				printer = func(s []string) {
					for _, source := range s {
						fmt.Println(source)
					}
				}
			}

			printer(sourcesList)
		},
	}
	cmdJobSources.Flags().BoolVar(&cmdJobSourcesPrintJSON, "json", false, "output as newline separated JSON")
	cmdJobSources.Flags().BoolVar(&cmdJobSourcesPrintCSV, "csv", false, "output as CSV with no header (source1, sourc2, ...)")

	var rootCmd = &cobra.Command{Use: "job-hunter-toolkit"}
	rootCmd.AddCommand(cmdJobPostings)
	rootCmd.AddCommand(cmdJobSources)
	rootCmd.Execute()
}
