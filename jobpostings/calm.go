package jobpostings

import (
	"context"
)

// GetCalmJobPostings finds JobPostings found at https:/lever.co
func GetCalmJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "calm")
}
