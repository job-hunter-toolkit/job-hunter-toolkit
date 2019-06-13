package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCatalyticJobPostings(t *testing.T) {
	jobPostings, err := GetCatalyticJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}