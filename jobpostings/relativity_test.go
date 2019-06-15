package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRelativityJobPostings(t *testing.T) {
	jobPostings, err := GetRelativityJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}