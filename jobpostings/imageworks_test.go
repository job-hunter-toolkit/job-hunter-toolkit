package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetImageworksJobPostings(t *testing.T) {
	jobPostings, err := GetImageworksJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
