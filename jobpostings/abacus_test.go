package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAbacusJobPostings(t *testing.T) {
	jobPostings, err := GetAbacusJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}