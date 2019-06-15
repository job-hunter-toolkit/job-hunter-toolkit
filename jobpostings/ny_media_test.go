package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNYMediaJobPostings(t *testing.T) {
	jobPostings, err := GetNYMediaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}