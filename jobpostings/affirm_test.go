package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAffirmJobPostings(t *testing.T) {
	jobPostings, err := GetAffirmJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}