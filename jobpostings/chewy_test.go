package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetChewyJobPostings(t *testing.T) {
	jobPostings, err := GetChewyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
