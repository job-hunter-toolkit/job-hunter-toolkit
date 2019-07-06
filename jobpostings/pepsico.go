package jobpostings

import (
	"context"
)

// GetPepsiCoJobPostings finds JobPostings found at https:/jibeapply.com
func GetPepsiCoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "pepsico")
}
