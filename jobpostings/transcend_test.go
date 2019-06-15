package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTranscendJobPostings(t *testing.T) {
	jobPostings, err := GetTranscendJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}