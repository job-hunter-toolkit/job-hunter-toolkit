package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTimeJobPostings(t *testing.T) {
	jobPostings, err := GetTimeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
