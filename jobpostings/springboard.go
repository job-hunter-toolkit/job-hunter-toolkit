package jobpostings

import (
	"context"
)

// GetSpringboardJobPostings finds JobPostings found at https:/lever.co
func GetSpringboardJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "springboard")
}
