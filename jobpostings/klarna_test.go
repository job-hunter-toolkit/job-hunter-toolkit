package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetKlarnaJobPostings(t *testing.T) {
	jobPostings, err := GetKlarnaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}