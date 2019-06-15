package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetIrisAutomationJobPostings(t *testing.T) {
	jobPostings, err := GetIrisAutomationJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
