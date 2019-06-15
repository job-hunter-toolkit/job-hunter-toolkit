package jobpostings

import (
	"context"
)

// GetBitnamiJobPostings finds JobPostings found at https:/lever.co
func GetBitnamiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "bitnami")
}
