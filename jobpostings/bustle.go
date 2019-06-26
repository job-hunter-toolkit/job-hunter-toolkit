package jobpostings

import (
	"context"
)

// GetBustleJobPostings finds JobPostings found at https:/lever.co
func GetBustleJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "bustle")
}
