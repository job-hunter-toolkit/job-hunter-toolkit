package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGet0xJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := Get0xJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
