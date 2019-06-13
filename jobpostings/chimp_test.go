package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetChimpJobPostings(t *testing.T) {
	jobPostings, err := GetChimpJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
