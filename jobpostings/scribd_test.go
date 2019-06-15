package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetScribdJobPostings(t *testing.T) {
	jobPostings, err := GetScribdJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}