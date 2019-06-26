package jobpostings

import (
	"context"
)

// GetSourceressJobPostings finds JobPostings found at https:/lever.co
func GetSourceressJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "sourceress")
}
