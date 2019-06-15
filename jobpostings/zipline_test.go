package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetZiplineJobPostings(t *testing.T) {
	jobPostings, err := GetZiplineJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}