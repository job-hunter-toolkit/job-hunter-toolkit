package jobpostings

import (
	"context"
)

// GetAxonJobPostings finds JobPostings found at https:/lever.co
func GetAxonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "axon")
}
