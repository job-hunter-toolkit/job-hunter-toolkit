package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetZyrisJobPostings(t *testing.T) {
	jobPostings, err := GetZyrisJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
