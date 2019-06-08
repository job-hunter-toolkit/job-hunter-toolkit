package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNewYorkTimesJobPostings(t *testing.T) {
	jobPostings, err := GetNewYorkTimesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
