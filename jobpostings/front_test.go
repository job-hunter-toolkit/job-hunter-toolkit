package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFrontJobPostings(t *testing.T) {
	jobPostings, err := GetFrontJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}