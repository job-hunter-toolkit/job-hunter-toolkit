package jobpostings

import (
	"context"
)

// GetForwardJobPostings finds JobPostings found at https:/lever.co
func GetForwardJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "goforward")
}
