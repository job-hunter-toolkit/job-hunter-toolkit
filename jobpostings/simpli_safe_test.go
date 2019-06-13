package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSimpliSafeJobPostings(t *testing.T) {
	jobPostings, err := GetSimpliSafeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}