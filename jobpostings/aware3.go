package jobpostings

import (
	"context"
)

// GetAware3JobPostings finds JobPostings found at https:/lever.co
func GetAware3JobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "aware3")
}
