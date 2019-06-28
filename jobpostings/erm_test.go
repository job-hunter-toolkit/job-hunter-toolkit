package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetERMJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetERMJobPostings(context.Background())

	if err != nil {
		t.Parallel()

		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
