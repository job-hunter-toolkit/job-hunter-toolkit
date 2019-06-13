package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLimeadeJobPostings(t *testing.T) {
	jobPostings, err := GetLimeadeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
