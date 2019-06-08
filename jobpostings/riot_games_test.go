package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRiotGamesJobPostings(t *testing.T) {
	jobPostings, err := GetRiotGamesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
