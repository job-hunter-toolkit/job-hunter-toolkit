package jobpostings

import (
	"context"
)

// GetSourceDJobPostings finds JobPostings found at https:/lever.co
func GetSourceDJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "sourced")
}
