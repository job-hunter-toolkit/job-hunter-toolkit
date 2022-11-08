package jobpostings

import (
	"context"
)

// GetWintermuteJobPostings finds JobPostings found at https://jobs.lever.co/wintermute-trading
func GetWintermuteJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "wintermute-trading")
}
