package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTablexiJobPostings(t *testing.T) {
	jobPostings, err := GetTablexiJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
