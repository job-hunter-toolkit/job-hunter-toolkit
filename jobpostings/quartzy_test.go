package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetQuartzyJobPostings(t *testing.T) {
	jobPostings, err := GetQuartzyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}