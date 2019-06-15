package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBlockstackJobPostings(t *testing.T) {
	jobPostings, err := GetBlockstackJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}