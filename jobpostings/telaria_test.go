package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTelariaJobPostings(t *testing.T) {
	jobPostings, err := GetTelariaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}