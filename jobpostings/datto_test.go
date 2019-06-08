package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDattoJobPostings(t *testing.T) {
	jobPostings, err := GetDattoJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
