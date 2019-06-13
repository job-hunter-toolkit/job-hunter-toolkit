package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPathAIJobPostings(t *testing.T) {
	jobPostings, err := GetPathAIJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}