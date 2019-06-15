package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetClerkyJobPostings(t *testing.T) {
	jobPostings, err := GetClerkyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}