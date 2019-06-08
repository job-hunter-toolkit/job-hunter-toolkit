package jobpostings

import (
	"context"
)

// GetCoinbaseJobPostings finds JobPostings found at https://greenhouse.io
func GetCoinbaseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "coinbase")
}
