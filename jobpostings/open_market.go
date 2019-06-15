package jobpostings

import (
	"context"
)

// GetOpenMarketJobPostings finds JobPostings found at https:/lever.co
func GetOpenMarketJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "openmarket")
}
