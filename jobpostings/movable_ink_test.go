package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetMovableInkJobPostings(t *testing.T) {
	jobPostings, err := GetMovableInkJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
