package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetWizelineJobPostings(t *testing.T) {
	jobPostings, err := GetWizelineJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}