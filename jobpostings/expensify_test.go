package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetExpensifyJobPostings(t *testing.T) {
	jobPostings, err := GetExpensifyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}