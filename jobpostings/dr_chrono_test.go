package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDrChronoJobPostings(t *testing.T) {
	jobPostings, err := GetDrChronoJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}