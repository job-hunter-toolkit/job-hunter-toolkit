package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetThunderTokenJobPostings(t *testing.T) {
	jobPostings, err := GetThunderTokenJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}