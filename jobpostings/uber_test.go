package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetUberJobPostings(t *testing.T) {
	jobPostings, err := GetUberJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
