package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetModernTimesBeerJobPostings(t *testing.T) {
	jobPostings, err := GetModernTimesBeerJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}