package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetJellyfishJobPostings(t *testing.T) {
	jobPostings, err := GetJellyfishJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}