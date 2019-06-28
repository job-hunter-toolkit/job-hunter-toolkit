package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAnchorageJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetAnchorageJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
