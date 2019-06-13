package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSecuronixJobPostings(t *testing.T) {
	jobPostings, err := GetSecuronixJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
