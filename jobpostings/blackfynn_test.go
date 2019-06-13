package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBlackfynnJobPostings(t *testing.T) {
	jobPostings, err := GetBlackfynnJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}