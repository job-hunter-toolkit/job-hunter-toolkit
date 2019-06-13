package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSauceLabsJobPostings(t *testing.T) {
	jobPostings, err := GetSauceLabsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
