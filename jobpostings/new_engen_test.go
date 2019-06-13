package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNewEngenJobPostings(t *testing.T) {
	jobPostings, err := GetNewEngenJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
