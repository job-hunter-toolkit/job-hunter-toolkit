package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPostmatesJobPostings(t *testing.T) {
	jobPostings, err := GetPostmatesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
