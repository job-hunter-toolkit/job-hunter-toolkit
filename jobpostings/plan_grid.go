package jobpostings

import (
	"context"
)

// GetPlanGridJobPostings finds JobPostings found at https:/lever.co
func GetPlanGridJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "plangrid")
}
