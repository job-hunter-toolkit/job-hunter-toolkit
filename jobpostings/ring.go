package jobpostings

import (
	"context"
)

// GetRingJobPostings finds JobPostings found at https:/lever.co
func GetRingJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "ring")
}