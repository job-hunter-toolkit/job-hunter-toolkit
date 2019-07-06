package jobpostings

import (
	"context"
)

// GetBJCJobPostings finds JobPostings found at https:/jibeapply.com
func GetBJCJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "bjc")
}
