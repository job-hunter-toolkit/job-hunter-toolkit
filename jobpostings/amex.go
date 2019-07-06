package jobpostings

import (
	"context"
)

// GetAmexJobPostings finds JobPostings found at https:/jibeapply.com
func GetAmexJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "amex")
}
