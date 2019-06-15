package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSignalJobPostings(t *testing.T) {
	jobPostings, err := GetSignalJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}