package jobpostings

import (
	"context"
)

// GetBinanceJobPostings finds JobPostings found at https:/lever.co
func GetBinanceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "binance")
}
