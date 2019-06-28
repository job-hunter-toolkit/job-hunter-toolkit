package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetEvernoteJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetEvernoteJobPostings(context.Background())

	if err != nil {
		t.Parallel()

		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
