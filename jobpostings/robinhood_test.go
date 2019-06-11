package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRobinhoodJobPostings(t *testing.T) {
	jobPostings, err := GetRobinhoodJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
