package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetInsightsoftwareJobPostings(t *testing.T) {
	jobPostings, err := GetInsightsoftwareJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
