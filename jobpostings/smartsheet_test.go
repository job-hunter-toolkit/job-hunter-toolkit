package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSmartsheetJobPostings(t *testing.T) {
	jobPostings, err := GetSmartsheetJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
