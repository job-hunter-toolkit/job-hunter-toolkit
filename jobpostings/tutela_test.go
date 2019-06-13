package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTutelaJobPostings(t *testing.T) {
	jobPostings, err := GetTutelaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
