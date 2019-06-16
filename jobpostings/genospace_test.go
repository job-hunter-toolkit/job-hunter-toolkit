package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetGenospaceJobPostings(t *testing.T) {
	jobPostings, err := GetGenospaceJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
