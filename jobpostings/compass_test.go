package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCompassJobPostings(t *testing.T) {
	jobPostings, err := GetCompassJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}