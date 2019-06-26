package jobpostings

import (
	"context"
)

// GetBirdJobPostings finds JobPostings found at https:/lever.co
func GetBirdJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "bird")
}
