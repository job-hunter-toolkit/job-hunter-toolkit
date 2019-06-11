package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSnapRaiseJobPostings(t *testing.T) {
	jobPostings, err := GetSnapRaiseJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
