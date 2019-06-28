package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetStauerJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetStauerJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
