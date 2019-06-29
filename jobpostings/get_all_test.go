package jobpostings

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGetAllJobPostings(t *testing.T) {
	// this test is probably the only that shouldn't be running in parallel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jobPostings := GetAllJobPostings(ctx)

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
