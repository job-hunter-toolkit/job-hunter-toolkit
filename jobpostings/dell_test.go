package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDellJobPostings(t *testing.T) {
	jobPostings, err := GetDellJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
