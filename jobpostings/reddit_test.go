package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRedditJobPostings(t *testing.T) {
	jobPostings, err := GetRedditJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
