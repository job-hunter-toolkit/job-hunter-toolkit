package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPetalJobPostings(t *testing.T) {
	jobPostings, err := GetPetalJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}