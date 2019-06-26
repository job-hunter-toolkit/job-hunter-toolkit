package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetOptivJobPostings(t *testing.T) {
	jobPostings, err := GetOptivJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
