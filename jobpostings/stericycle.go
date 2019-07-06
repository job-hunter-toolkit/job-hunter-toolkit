package jobpostings

import (
	"context"
)

// GetStericycleJobPostings finds JobPostings found at https:/jibeapply.com
func GetStericycleJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "stericycle")
}
