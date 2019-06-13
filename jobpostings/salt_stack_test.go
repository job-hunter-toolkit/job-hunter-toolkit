package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSaltStackJobPostings(t *testing.T) {
	jobPostings, err := GetSaltStackJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
