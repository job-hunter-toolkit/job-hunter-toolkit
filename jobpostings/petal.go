package jobpostings

import (
	"context"
)

// GetPetalJobPostings finds JobPostings found at https:/lever.co
func GetPetalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "petalcard")
}
