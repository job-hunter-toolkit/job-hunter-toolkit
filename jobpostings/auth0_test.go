package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAuth0JobPostings(t *testing.T) {
	jobPostings, err := GetAuth0JobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}