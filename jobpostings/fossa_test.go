package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFossaJobPostings(t *testing.T) {
	jobPostings, err := GetFossaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}