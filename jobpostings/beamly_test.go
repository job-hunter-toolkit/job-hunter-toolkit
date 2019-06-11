package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBeamlyJobPostings(t *testing.T) {
	jobPostings, err := GetBeamlyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}