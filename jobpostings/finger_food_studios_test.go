package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFingerFoodStudiosJobPostings(t *testing.T) {
	jobPostings, err := GetFingerFoodStudiosJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}