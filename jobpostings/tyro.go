package jobpostings

import (
	"context"
)

// GetTyroJobPostings finds JobPostings found at https:/lever.co
func GetTyroJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "tyro")
}
