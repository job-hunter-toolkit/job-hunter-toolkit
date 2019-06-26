package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestSmartRecruitersJobPostings(t *testing.T) {
	jobPostings, err := getSmartRecruitersJobsPostingsFor(context.Background(), "optiv")
	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
