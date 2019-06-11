package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFirstJobPostings(t *testing.T) {
	jobPostings, err := GetFirstJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
