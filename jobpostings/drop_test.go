package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDropJobPostings(t *testing.T) {
	jobPostings, err := GetDropJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
