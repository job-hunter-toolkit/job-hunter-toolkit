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

var sourcesList = []string{
	"21st_century_fox",
	"3m",
	"adobe",
	"aes",
	"airbnb",
	"bose",
	"buzzfeed",
	"cae",
	"carbon_black",
	"casper",
	"cisco_meraki",
	"city_and_county_of_denver",
	"cloudreach",
	"coinbase",
	"conrell_university",
	"datto",
	"dell",
	"dnb",
	"duo_security",
	"elastic",
	"epic_games",
	"erm",
	"evernote",
	"expedia",
	"fico",
	"gates_foundation",
	"github",
	"gitlab",
	"hashicorp",
	"hello_fresh",
	"imageworks",
	"instacart",
	"jda",
	"job_posting",
	"kantar",
	"live_nation",
	"lyft",
	"major_league_baseball",
	"mastercard",
	"nationwide",
	"new_york_times",
	"nvidia",
	"okta",
	"pae",
	"pfizer",
	"pinterest",
	"rapid7",
	"reddit",
	"riot_games",
	"rolls_royce",
	"salesforce",
	"samsung",
	"squarespace",
	"state_steet",
	"stripe",
	"sumo_logic",
	"survey_monkey",
	"symantec",
	"taco_bell",
	"toptal",
	"trip_advisor",
	"twilio",
	"uber",
	"uberether",
	"unisys",
	"venmo",
	"veritas",
	"verizon_media",
	"vox_media",
	"whole_foods",
	"zenefits",
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
