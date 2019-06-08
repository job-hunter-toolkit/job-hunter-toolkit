package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetUnisysJobPostings(t *testing.T) {
	jobPostings, err := GetUnisysJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
