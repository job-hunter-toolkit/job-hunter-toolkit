package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetReifyHealthJobPostings(t *testing.T) {
	jobPostings, err := GetReifyHealthJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}