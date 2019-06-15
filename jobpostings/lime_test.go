package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLimeJobPostings(t *testing.T) {
	jobPostings, err := GetLimeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}