package jobpostings

import (
	"context"
)

// GetCompassGroupJobPostings finds JobPostings found at https:/jibeapply.com
func GetCompassGroupJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "compassgroup")
}
