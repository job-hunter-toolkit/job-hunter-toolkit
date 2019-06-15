package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBigHealthJobPostings(t *testing.T) {
	jobPostings, err := GetBigHealthJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}