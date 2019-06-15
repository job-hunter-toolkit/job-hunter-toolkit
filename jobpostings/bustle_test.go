package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBustleJobPostings(t *testing.T) {
	jobPostings, err := GetBustleJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}