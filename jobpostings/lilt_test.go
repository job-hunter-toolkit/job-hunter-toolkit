package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLiltJobPostings(t *testing.T) {
	jobPostings, err := GetLiltJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}