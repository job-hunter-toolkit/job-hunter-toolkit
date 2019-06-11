package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLogitechJobPostings(t *testing.T) {
	jobPostings, err := GetLogitechJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}