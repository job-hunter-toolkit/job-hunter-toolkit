package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetReturntocorpJobPostings(t *testing.T) {
	jobPostings, err := GetReturntocorpJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}