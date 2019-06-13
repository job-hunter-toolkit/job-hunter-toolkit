package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSpotHeroJobPostings(t *testing.T) {
	jobPostings, err := GetSpotHeroJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
