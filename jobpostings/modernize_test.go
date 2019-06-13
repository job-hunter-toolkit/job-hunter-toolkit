package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetModernizeJobPostings(t *testing.T) {
	jobPostings, err := GetModernizeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}