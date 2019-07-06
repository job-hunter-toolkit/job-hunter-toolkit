package jobpostings

import (
	"context"
)

// GetMarriottJobPostings finds JobPostings found at https:/jibeapply.com
func GetMarriottJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "marriott")
}
