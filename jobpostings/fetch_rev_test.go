package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFetchRevJobPostings(t *testing.T) {
	jobPostings, err := GetFetchRevJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}
