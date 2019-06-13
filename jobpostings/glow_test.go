package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetGlowJobPostings(t *testing.T) {
	jobPostings, err := GetGlowJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}