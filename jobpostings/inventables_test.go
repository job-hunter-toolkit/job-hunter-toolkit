package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetInventablesJobPostings(t *testing.T) {
	jobPostings, err := GetInventablesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
