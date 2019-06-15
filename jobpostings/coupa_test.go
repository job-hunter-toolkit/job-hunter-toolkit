package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCoupaJobPostings(t *testing.T) {
	jobPostings, err := GetCoupaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}