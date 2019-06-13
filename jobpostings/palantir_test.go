package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPalantirJobPostings(t *testing.T) {
	jobPostings, err := GetPalantirJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
