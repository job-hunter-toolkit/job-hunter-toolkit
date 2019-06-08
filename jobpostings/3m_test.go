package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGet3MJobPostings(t *testing.T) {
	jobPostings, err := Get3MJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
