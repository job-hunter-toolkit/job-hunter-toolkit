package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSiftJobPostings(t *testing.T) {
	jobPostings, err := GetSiftJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
