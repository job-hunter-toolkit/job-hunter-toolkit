package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOmadaHealthJobPostings(t *testing.T) {
	jobPostings, err := GetOmadaHealthJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
