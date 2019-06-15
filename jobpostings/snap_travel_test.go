package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSnapTravelJobPostings(t *testing.T) {
	jobPostings, err := GetSnapTravelJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}