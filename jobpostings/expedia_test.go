package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetExpediaJobPostings(t *testing.T) {
	jobPostings, err := GetExpediaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}
