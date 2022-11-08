package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNeo4jJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetNeo4jJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
