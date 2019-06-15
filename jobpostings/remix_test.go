package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRemixJobPostings(t *testing.T) {
	jobPostings, err := GetRemixJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}