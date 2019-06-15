package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOpendoorJobPostings(t *testing.T) {
	jobPostings, err := GetOpendoorJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}