package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetStarshipJobPostings(t *testing.T) {
	jobPostings, err := GetStarshipJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}