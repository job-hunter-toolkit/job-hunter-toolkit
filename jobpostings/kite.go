package jobpostings

import (
	"context"
)

// GetKiteJobPostings finds JobPostings found at https:/lever.co
func GetKiteJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "kite")
}
