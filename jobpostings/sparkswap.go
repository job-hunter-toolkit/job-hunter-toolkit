package jobpostings

import (
	"context"
)

// GetSparkswapJobPostings finds JobPostings found at https:/lever.co
func GetSparkswapJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "sparkswap")
}
