package jobpostings

import (
	"context"
)

// GetNashJobPostings finds JobPostings found at https:/lever.co
func GetNashJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "nash.io")
}
