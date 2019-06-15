package jobpostings

import (
	"context"
)

// GetReifyHealthJobPostings finds JobPostings found at https:/lever.co
func GetReifyHealthJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "reifyhealth")
}
