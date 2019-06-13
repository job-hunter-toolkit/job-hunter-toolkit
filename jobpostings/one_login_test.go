package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOneLoginJobPostings(t *testing.T) {
	jobPostings, err := GetOneLoginJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
