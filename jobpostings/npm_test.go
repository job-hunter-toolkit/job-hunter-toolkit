package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNPMJobPostings(t *testing.T) {
	jobPostings, err := GetNPMJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}