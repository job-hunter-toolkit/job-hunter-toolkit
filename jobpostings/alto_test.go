package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAltoJobPostings(t *testing.T) {
	jobPostings, err := GetAltoJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}