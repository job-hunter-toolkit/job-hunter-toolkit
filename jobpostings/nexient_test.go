package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNexientJobPostings(t *testing.T) {
	jobPostings, err := GetNexientJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
