package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTeamableJobPostings(t *testing.T) {
	jobPostings, err := GetTeamableJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}