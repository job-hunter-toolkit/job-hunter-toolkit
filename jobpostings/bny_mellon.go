package jobpostings

import (
	"context"
)

// GetBYNMellonJobPostings finds JobPostings found at https:/jibeapply.com
func GetBYNMellonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "bnymellon")
}
