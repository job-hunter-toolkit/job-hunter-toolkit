package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTwitchJobPostings(t *testing.T) {
	jobPostings, err := GetTwitchJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}