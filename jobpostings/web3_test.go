package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetWeb3JobPostings(t *testing.T) {
	jobPostings, err := GetWeb3JobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}