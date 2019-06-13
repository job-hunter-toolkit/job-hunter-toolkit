package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetUpstartJobPostings(t *testing.T) {
	jobPostings, err := GetUpstartJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
