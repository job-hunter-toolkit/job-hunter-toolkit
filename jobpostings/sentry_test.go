package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSentryJobPostings(t *testing.T) {
	jobPostings, err := GetSentryJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
