package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetShoesJobPostings(t *testing.T) {
	jobPostings, err := GetShoesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
