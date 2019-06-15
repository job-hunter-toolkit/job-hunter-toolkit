package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAmyrisJobPostings(t *testing.T) {
	jobPostings, err := GetAmyrisJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}