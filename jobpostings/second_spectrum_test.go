package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSecondSpectrumJobPostings(t *testing.T) {
	jobPostings, err := GetSecondSpectrumJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}