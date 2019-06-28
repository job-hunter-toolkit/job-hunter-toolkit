package jobpostings

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGetAllJobPostings(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jobPostings := GetAllJobPostings(ctx)

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
