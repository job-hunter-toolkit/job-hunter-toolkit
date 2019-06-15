package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetMakeSpaceJobPostings(t *testing.T) {
	jobPostings, err := GetMakeSpaceJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
