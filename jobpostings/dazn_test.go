package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDaznJobPostings(t *testing.T) {
	jobPostings, err := GetDaznJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}