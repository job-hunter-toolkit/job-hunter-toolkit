package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetEssentialJobPostings(t *testing.T) {
	jobPostings, err := GetEssentialJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}