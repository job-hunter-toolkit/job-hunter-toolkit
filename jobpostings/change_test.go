package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetChangeJobPostings(t *testing.T) {
	jobPostings, err := GetChangeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}