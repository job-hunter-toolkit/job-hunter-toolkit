package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTripleByteJobPostings(t *testing.T) {
	jobPostings, err := GetTripleByteJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}