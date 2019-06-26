package jobpostings

import (
	"context"
)

// GetStarcityJobPostings finds JobPostings found at https:/lever.co
func GetStarcityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "starcity")
}
