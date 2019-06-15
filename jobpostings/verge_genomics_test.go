package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVergeGenomicsJobPostings(t *testing.T) {
	jobPostings, err := GetVergeGenomicsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}