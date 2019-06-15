package jobpostings

import (
	"context"
)

// GetRemixJobPostings finds JobPostings found at https:/lever.co
func GetRemixJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "remix")
}
