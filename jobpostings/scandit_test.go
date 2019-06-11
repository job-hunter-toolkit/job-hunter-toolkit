package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetScanditJobPostings(t *testing.T) {
	jobPostings, err := GetScanditJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
