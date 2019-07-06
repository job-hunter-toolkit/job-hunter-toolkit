package jobpostings

import (
	"context"
)

// GetEatonJobPostings finds JobPostings found at https:/jibeapply.com
func GetEatonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "eaton")
}
