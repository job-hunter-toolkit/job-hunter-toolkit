package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTalaJobPostings(t *testing.T) {
	jobPostings, err := GetTalaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
