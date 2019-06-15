package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSnapJobPostings(t *testing.T) {
	jobPostings, err := GetSnapJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
