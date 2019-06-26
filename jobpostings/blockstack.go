package jobpostings

import (
	"context"
)

// GetBlockstackJobPostings finds JobPostings found at https:/lever.co
func GetBlockstackJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "blockstack")
}
