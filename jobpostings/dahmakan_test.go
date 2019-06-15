package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDahmakanJobPostings(t *testing.T) {
	jobPostings, err := GetDahmakanJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
