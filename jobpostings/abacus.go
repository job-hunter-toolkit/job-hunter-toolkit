package jobpostings

import (
	"context"
)

// GetAbacusJobPostings finds JobPostings found at https:/lever.co
func GetAbacusJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "abacus")
}
