package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetHelloFreshJobPostings(t *testing.T) {
	jobPostings, err := GetHelloFreshJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
