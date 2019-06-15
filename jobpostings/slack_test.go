package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSlackJobPostings(t *testing.T) {
	jobPostings, err := GetSlackJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
