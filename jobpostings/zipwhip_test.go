package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetZipwhipJobPostings(t *testing.T) {
	jobPostings, err := GetZipwhipJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
