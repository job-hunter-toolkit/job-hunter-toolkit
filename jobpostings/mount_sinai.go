package jobpostings

import (
	"context"
)

// GetMountSinaiJobPostings finds JobPostings found at https:/jibeapply.com
func GetMountSinaiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "mountsinai")
}
