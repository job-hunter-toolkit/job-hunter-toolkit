package jobpostings

import (
	"time"
	"context"
	"fmt"
	"testing"
)

func TestGetAllJobPostings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jobPostings := GetAllJobPostings(ctx)

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
