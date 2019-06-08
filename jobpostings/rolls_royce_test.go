package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRollsRoyceJobPostings(t *testing.T) {
	jobPostings, err := GetRollsRoyceJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
