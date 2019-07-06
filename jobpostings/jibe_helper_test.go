package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestJibeJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := getJibeJobsFor(context.Background(), "amex")

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {

		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
