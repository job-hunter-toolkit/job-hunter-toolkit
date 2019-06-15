package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetWikimediaJobPostings(t *testing.T) {
	jobPostings, err := GetWikimediaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
