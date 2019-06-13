package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRelayrJobPostings(t *testing.T) {
	jobPostings, err := GetRelayrJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
