package jobpostings

import (
	"context"
)

// GetMcDonaldsJobPostings finds JobPostings found at https:/jibeapply.com
func GetMcDonaldsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "mcdonalds")
}
