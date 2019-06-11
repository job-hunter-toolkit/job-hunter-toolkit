package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTimeIncJobPostings(t *testing.T) {
	jobPostings, err := GetTimeIncJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}