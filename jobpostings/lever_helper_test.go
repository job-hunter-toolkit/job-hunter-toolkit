package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestLeverJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := getLeverJobsFor(context.Background(), "npm")
	//jobPostings, err := getLeverJobsFor(context.Background(), "twitch")

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
