package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCoreScientificJobPostings(t *testing.T) {
	jobPostings, err := GetCoreScientificJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
