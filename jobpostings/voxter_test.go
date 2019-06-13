package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVoxterJobPostings(t *testing.T) {
	jobPostings, err := GetVoxterJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}