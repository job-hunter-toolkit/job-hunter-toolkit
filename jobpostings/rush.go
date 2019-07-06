package jobpostings

import (
	"context"
)

// GetRushJobPostings finds JobPostings found at https:/jibeapply.com
func GetRushJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "rush")
}
