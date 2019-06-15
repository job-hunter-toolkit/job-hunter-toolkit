package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetScaleAIJobPostings(t *testing.T) {
	jobPostings, err := GetScaleAIJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}