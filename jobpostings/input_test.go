package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetInputJobPostings(t *testing.T) {
	jobPostings, err := GetInputJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
