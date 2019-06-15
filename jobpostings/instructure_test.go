package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetInstructureJobPostings(t *testing.T) {
	jobPostings, err := GetInstructureJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}