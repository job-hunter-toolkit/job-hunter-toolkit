package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNurxJobPostings(t *testing.T) {
	jobPostings, err := GetNurxJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
