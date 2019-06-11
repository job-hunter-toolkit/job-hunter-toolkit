package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAirMapJobPostings(t *testing.T) {
	jobPostings, err := GetAirMapJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
