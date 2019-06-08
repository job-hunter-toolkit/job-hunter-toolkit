package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetMajorLeagueBaseballPostings(t *testing.T) {
	jobPostings, err := GetMajorLeagueBaseballPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
