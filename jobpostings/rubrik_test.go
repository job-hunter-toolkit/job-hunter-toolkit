package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRubrikJobPostings(t *testing.T) {
	jobPostings, err := GetRubrikJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}