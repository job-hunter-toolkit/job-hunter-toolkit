package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetEvercommerceJobPostings(t *testing.T) {
	jobPostings, err := GetEvercommerceJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}
