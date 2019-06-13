package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSwatJobPostings(t *testing.T) {
	jobPostings, err := GetSwatJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
