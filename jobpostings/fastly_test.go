package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFastlyJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetFastlyJobPostings(context.Background())

	if err != nil {
		t.Parallel()

		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
