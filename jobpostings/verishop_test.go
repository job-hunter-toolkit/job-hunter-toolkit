package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVerishopJobPostings(t *testing.T) {
	jobPostings, err := GetVerishopJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}