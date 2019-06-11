package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBittrexJobPostings(t *testing.T) {
	jobPostings, err := GetBittrexJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}