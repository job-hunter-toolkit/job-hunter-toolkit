package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSecurityTrailsJobPostings(t *testing.T) {
	jobPostings, err := GetSecurityTrailsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
