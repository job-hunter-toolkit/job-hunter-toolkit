package jobpostings

import (
	"context"
)

// GetReturntocorpJobPostings finds JobPostings found at https:/lever.co
func GetReturntocorpJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "returntocorp")
}
