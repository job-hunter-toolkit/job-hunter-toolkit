package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetWellframeJobPostings(t *testing.T) {
	jobPostings, err := GetWellframeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}