package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBuzzFeedJobPostings(t *testing.T) {
	jobPostings, err := GetBuzzFeedJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
