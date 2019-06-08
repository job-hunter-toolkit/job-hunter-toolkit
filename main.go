package main

import (
	"fmt"
	"context"
	"os"
	"os/signal"
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

	var cmdJobPostings = &cobra.Command{
		Use:   "job-postings",
		Short: "Find job postings from various companies",
		Run: func(cmd *cobra.Command, args []string) {
			for jobPosting := range jobpostings.GetAllJobPostings(context.Background()) {
				fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
			}
		},
	}

	var rootCmd = &cobra.Command{Use: "job-hunter-toolkit"}
	rootCmd.AddCommand(cmdJobPostings)
	rootCmd.Execute()
}