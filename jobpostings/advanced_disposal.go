package jobpostings

import (
	"context"
)

// GetAdvancedDisposalJobPostings finds JobPostings found at https:/jibeapply.com
func GetAdvancedDisposalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "advanceddisposal")
}
