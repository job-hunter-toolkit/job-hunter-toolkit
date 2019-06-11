package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVerifoneJobPostings(t *testing.T) {
	jobPostings, err := GetVerifoneJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}