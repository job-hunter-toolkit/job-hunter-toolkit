package main

import (
	"fmt"
	"context"
	"os"
	"os/signal"
	"encoding/json"
	"encoding/csv"
	"github.com/spf13/cobra"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobpostings"
)

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

	var rootCmd = &cobra.Command{Use: "job-hunter-toolkit"}
	rootCmd.AddCommand(cmdJobPostings)
	rootCmd.Execute()
}