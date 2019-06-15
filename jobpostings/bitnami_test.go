package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBitnamiJobPostings(t *testing.T) {
	jobPostings, err := GetBitnamiJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}