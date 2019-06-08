package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetStripeJobPostings(t *testing.T) {
	jobPostings, err := GetStripeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
