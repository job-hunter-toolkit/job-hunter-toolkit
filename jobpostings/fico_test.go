package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFICOJobPostings(t *testing.T) {
	jobPostings, err := GetFICOJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}
