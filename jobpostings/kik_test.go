package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetKikJobPostings(t *testing.T) {
	jobPostings, err := GetKikJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}