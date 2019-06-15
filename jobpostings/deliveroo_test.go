package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDeliverooJobPostings(t *testing.T) {
	jobPostings, err := GetDeliverooJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
