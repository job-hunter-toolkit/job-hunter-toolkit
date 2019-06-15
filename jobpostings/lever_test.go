package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLeverJobPostings(t *testing.T) {
	jobPostings, err := GetLeverJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}