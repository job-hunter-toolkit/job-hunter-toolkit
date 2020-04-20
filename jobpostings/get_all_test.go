package jobpostings

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGetAllJobPostings(t *testing.T) {
	// this test is probably the only that shouldn't be running in parallel
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	jobPostings := GetAllJobPostings(ctx)

	for jobPosting := range jobPostings {
		// spot check during testing
		if jobPosting.URL == "" {
			t.Fatalf("URL is empty for: %#+v", jobPosting)
		}
		if jobPosting.Location == "" {
			t.Fatalf("Location is empty for: %#+v", jobPosting)
		}
		if jobPosting.Title == "" {
			t.Fatalf("Title is empty for: %#+v", jobPosting)
		}
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
