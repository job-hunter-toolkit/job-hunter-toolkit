package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetZenefitsJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetZenefitsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
