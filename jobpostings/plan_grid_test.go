package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPlanGridJobPostings(t *testing.T) {
	jobPostings, err := GetPlanGridJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}