package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNS1JobPostings(t *testing.T) {
	jobPostings, err := GetNS1JobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}