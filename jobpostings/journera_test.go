package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetJourneraJobPostings(t *testing.T) {
	jobPostings, err := GetJourneraJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}