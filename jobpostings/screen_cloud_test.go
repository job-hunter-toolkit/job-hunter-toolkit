package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetScreenCloudJobPostings(t *testing.T) {
	jobPostings, err := GetScreenCloudJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
