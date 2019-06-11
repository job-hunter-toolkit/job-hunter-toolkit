package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestJobviteJobPostings(t *testing.T) {
	jobPostings, err := getJobviteJobsFor(context.Background(), "spotify")

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
