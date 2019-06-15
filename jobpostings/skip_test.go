package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSkipJobPostings(t *testing.T) {
	jobPostings, err := GetSkipJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}