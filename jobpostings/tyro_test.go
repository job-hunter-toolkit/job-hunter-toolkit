package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTyroJobPostings(t *testing.T) {
	jobPostings, err := GetTyroJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}