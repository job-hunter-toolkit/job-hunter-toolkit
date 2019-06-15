package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDarkstoreJobPostings(t *testing.T) {
	jobPostings, err := GetDarkstoreJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}