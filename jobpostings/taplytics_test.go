package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTaplyticsJobPostings(t *testing.T) {
	jobPostings, err := GetTaplyticsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}