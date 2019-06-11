package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPatreonJobPostings(t *testing.T) {
	jobPostings, err := GetPatreonJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}