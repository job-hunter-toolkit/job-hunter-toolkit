package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetUberEtherJobPostings(t *testing.T) {
	jobPostings, err := GetUberEtherJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
