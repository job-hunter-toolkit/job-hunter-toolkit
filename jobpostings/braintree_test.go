package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBraintreeJobPostings(t *testing.T) {
	jobPostings, err := GetBraintreeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
