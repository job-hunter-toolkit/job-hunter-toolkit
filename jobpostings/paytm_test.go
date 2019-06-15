package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPaytmJobPostings(t *testing.T) {
	jobPostings, err := GetPaytmJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}