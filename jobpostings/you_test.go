package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetYouJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetYouJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
