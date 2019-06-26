package jobpostings

import (
	"context"
)

// GetOriginJobPostings finds JobPostings found at https:/lever.co
func GetOriginJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "origin")
}
