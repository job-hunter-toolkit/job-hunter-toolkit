package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetHopsyJobPostings(t *testing.T) {
	jobPostings, err := GetHopsyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}