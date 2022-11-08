package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPPFAJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetPPFAJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
