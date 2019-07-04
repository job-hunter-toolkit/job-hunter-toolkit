package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCapitalOneJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetCapitalOneJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
