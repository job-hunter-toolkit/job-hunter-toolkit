package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetEpicGamesJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetEpicGamesJobPostings(context.Background())

	if err != nil {
		t.Parallel()

		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
