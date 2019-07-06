package jobpostings

import (
	"context"
)

// GetFedexJobPostings finds JobPostings found at https:/jibeapply.com
func GetFedexJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "fedex")
}
