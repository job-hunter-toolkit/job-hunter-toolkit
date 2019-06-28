package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetResearchGateJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetResearchGateJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
