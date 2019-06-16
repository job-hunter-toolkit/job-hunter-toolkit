package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestHireWithGoogleJobPostings(t *testing.T) {
	jobPostings, err := getHireWithGoogleJobPostingsFor(context.Background(), "duolingocom")
	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
