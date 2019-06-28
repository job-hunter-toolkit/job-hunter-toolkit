package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestBambooHRJobPostings(t *testing.T) {
	t.Parallel()

	//broken : //jobPostings, err := getBambooHRJobsFor(context.Background(), "assembly")
	jobPostings, err := getBambooHRJobsFor(context.Background(), "azerion")
	//jobPostings, err := getBambooHRJobsFor(context.Background(), "zerofox")
	//jobPostings, err := getBambooHRJobsFor(context.Background(), "iota")

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
