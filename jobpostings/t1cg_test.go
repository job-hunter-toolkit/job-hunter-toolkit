package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetT1CGJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetT1CGJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
