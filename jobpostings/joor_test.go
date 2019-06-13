package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetJoorJobPostings(t *testing.T) {
	jobPostings, err := GetJoorJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}