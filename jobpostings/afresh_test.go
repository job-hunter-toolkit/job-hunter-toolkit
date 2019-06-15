package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAfreshJobPostings(t *testing.T) {
	jobPostings, err := GetAfreshJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}