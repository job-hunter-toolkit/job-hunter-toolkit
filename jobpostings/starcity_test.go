package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetStarcityJobPostings(t *testing.T) {
	jobPostings, err := GetStarcityJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}