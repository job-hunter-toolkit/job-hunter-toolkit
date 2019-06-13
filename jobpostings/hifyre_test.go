package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetHifyreJobPostings(t *testing.T) {
	jobPostings, err := GetHifyreJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
