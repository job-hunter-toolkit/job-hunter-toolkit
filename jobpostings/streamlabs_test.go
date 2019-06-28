package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetStreamlabsJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetStreamlabsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
