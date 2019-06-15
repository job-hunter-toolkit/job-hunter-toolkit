package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFictivJobPostings(t *testing.T) {
	jobPostings, err := GetFictivJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}