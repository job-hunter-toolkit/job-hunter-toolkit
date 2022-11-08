package jobpostings

import (
	"context"
)

// GetTiketJobPostings finds JobPostings found at https://jobs.lever.co/tiket
func GetTiketJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "tiket")
}
