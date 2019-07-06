package jobpostings

import (
	"context"
)

// GetComcastJobPostings finds JobPostings found at https:/jibeapply.com
func GetComcastJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "comcast")
}
