package jobpostings

import (
	"context"
	"testing"
	"time"
)

func TestGetAllJobPostings(t *testing.T) {
	// this test is probably the only that shouldn't be running in parallel
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	jobPostings := GetAllJobPostings(ctx)

	counter := int64(0)

	for jobPosting := range jobPostings {
		// keep track of the total number of job postings
		counter++
		// t.Logf("%d jobs found", counter)
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
		// fmt.Printf("%v\r", counter)
		// fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}

	t.Logf("Total job posings: %d", counter)
}
