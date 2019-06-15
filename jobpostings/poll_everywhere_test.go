package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPollEverywhereJobPostings(t *testing.T) {
	jobPostings, err := GetPollEverywhereJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}