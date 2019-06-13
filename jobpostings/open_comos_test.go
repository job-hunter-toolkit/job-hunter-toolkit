package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOpenCosmosJobPostings(t *testing.T) {
	jobPostings, err := GetOpenCosmosJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
