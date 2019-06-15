package jobpostings

import (
	"context"
)

// GetPlacepassJobPostings finds JobPostings found at https:/lever.co
func GetPlacepassJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "placepass")
}
