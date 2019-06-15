package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetReplateJobPostings(t *testing.T) {
	jobPostings, err := GetReplateJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}