package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetThunderTokenJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetThunderTokenJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
