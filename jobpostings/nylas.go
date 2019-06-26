package jobpostings

import (
	"context"
)

// GetNylasJobPostings finds JobPostings found at https:/lever.co
func GetNylasJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "nylas")
}
