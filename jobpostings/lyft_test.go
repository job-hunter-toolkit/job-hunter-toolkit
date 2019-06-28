package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLyftJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetLyftJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
