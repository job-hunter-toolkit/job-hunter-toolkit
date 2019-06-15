package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCalmJobPostings(t *testing.T) {
	jobPostings, err := GetCalmJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}