package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSumoLogicJobPostings(t *testing.T) {
	jobPostings, err := GetSumoLogicJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
