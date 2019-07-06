package jobpostings

import (
	"context"
)

// GetBallMetalpackJobPostings finds JobPostings found at https:/jibeapply.com
func GetBallMetalpackJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "ballmetalpack")
}
