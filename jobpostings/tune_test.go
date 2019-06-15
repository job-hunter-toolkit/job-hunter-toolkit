package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTuneJobPostings(t *testing.T) {
	jobPostings, err := GetTuneJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}