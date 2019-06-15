package jobpostings

import (
	"context"
)

// GetGoatJobPostings finds JobPostings found at https:/lever.co
func GetGoatJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "goat")
}
