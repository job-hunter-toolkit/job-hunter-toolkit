package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFondJobPostings(t *testing.T) {
	jobPostings, err := GetFondJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}