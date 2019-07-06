package jobpostings

import (
	"context"
)

// GetBelleTireJobPostings finds JobPostings found at https:/jibeapply.com
func GetBelleTireJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "belletire")
}
