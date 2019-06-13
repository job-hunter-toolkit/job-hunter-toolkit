package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVewdJobPostings(t *testing.T) {
	jobPostings, err := GetVewdJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
