package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBombasJobPostings(t *testing.T) {
	jobPostings, err := GetBombasJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}