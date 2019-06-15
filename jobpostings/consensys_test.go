package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetConsensysJobPostings(t *testing.T) {
	jobPostings, err := GetConsensysJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
