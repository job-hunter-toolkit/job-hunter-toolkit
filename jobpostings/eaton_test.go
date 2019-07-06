package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetEatonJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetEatonJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
