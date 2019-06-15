package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetForwardJobPostings(t *testing.T) {
	jobPostings, err := GetForwardJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}