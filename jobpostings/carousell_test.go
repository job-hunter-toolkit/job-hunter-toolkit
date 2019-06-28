package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCarousellJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetCarousellJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
