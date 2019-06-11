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
	"alto",
	"anchorage",
	"beamly",
	"better_up",
	"bittrex",
	"bombas",
	"bose",
	"braintree",
	"buildium",
	"buzzfeed",
	"cae",
	"canonical",
	"carbon_black",
	"carousell",
	"casper",
	"cb_insights",
	"cheddar",
	"cisco_meraki",
	"city_and_county_of_denver",
	"cloudflare",
	"cloudreach",
	"coinbase",
	"conrell_university",
	"core_scientific",
	"curalate",
	"datto",
	"dell",
	"digital_ocean",
	"dnb",
	"dots",
	"drop",
	"duo_security",
	"early_warning",
	"elastic",
	"epic_games",
	"erm",
	"evernote",
	"expedia",
	"expensify",
	"farmers_buisness_network",
	"fastly",
	"fico",
	"first",
	"gates_foundation",
	"get_all",
	"github",
	"gitlab",
	"go_daddy",
	"gridspace",
	"hashicorp",
	"hello_fresh",
	"imageworks",
	"input",
	"instacart",
	"inventables",
	"invitae",
	"jda",
	"job_posting",
	"kantar",
	"kayak",
	"kinnek",
	"light_step",
	"live_nation",
	"logitech",
	"lyft",
	"lyric",
	"magic_leap",
	"major_league_baseball",
	"mastercard",
	"maven_clinic",
	"narvar",
	"nationwide",
	"netlify",
	"new_york_times",
	"nexient",
	"nvidia",
	"okta",
	"omada_health",
	"omaze",
	"pae",
	"palo_alto_networks",
	"pathlight",
	"patreon",
	"pax",
	"pfizer",
	"pinterest",
	"policygenius",
	"predictive_index",
	"psi_kick",
	"qualys",
	"rapid7",
	"reddit",
	"riot_games",
	"robinhood",
	"rolls_royce",
	"salesforce",
	"samsara",
	"samsung",
	"scandit",
	"sentry",
	"shoes",
	"snap_raise",
	"spacex",
	"spotify",
	"squarespace",
	"state_steet",
	"stauer",
	"strava",
	"stripe",
	"sumo_logic",
	"survey_monkey",
	"symantec",
	"tableau",
	"taco_bell",
	"telaria",
	"time_inc",
	"toptal",
	"trip_advisor",
	"twilio",
	"uber",
	"uberether",
	"udacity",
	"unisys",
	"venafi",
	"venmo",
	"verifone",
	"verishop",
	"veritas",
	"verizon_media",
	"vice",
	"vox_media",
	"whisper",
	"whole_foods",
	"xylem",
	"zenefits",
	"zume",
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
