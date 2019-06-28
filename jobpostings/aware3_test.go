package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAware3JobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetAware3JobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
