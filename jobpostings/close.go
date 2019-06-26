package jobpostings

import (
	"context"
)

// GetCloseJobPostings finds JobPostings found at https:/lever.co
func GetCloseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "close.io")
}
