package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNashJobPostings(t *testing.T) {
	jobPostings, err := GetNashJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}