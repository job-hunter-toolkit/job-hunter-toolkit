package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSemaphoreSolutionsJobPostings(t *testing.T) {
	jobPostings, err := GetSemaphoreSolutionsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}