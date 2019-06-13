package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPushpayJobPostings(t *testing.T) {
	jobPostings, err := GetPushpayJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
