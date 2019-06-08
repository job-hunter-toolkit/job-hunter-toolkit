package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVoxMediaJobPostings(t *testing.T) {
	jobPostings, err := GetVoxMediaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
