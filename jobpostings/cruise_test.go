package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCruiseJobPostings(t *testing.T) {
	jobPostings, err := GetCruiseJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
