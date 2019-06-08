package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTwilioJobPostings(t *testing.T) {
	jobPostings, err := GetTwilioJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
