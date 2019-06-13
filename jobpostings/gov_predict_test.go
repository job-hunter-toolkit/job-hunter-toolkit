package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetGovPredictJobPostings(t *testing.T) {
	jobPostings, err := GetGovPredictJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
