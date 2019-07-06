package jobpostings

import (
	"context"
)

// GetLairdJobPostings finds JobPostings found at https:/jibeapply.com
func GetLairdJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "laird")
}
