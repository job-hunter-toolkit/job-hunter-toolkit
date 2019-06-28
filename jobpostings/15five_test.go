package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGet15FiveJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := Get15FiveJobPostings(context.Background())

	if err != nil {
		t.Parallel()

		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
