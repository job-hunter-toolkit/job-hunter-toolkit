package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetMetalToadJobPostings(t *testing.T) {
	jobPostings, err := GetMetalToadJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
