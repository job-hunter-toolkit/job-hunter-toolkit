package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetZeroFoxJobPostings(t *testing.T) {
	jobPostings, err := GetZeroFoxJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
