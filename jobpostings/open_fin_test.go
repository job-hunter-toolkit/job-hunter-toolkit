package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOpenFinJobPostings(t *testing.T) {
	jobPostings, err := GetOpenFinJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
