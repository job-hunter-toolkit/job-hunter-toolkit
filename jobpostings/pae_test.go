package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPAEJobPostings(t *testing.T) {
	jobPostings, err := GetPAEJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
