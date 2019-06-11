package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOmazeJobPostings(t *testing.T) {
	jobPostings, err := GetOmazeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
