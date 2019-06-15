package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetKiteJobPostings(t *testing.T) {
	jobPostings, err := GetKiteJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}