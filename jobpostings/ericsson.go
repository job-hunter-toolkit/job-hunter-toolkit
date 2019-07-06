package jobpostings

import (
	"context"
)

// GetEricssonJobPostings finds JobPostings found at https:/jibeapply.com
func GetEricssonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "ericsson")
}
