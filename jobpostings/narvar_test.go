package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNarvarJobPostings(t *testing.T) {
	jobPostings, err := GetNarvarJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
