package jobpostings

import (
	"context"
)

// GetIroncladJobPostings finds JobPostings found at https:/lever.co
func GetIroncladJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "ironcladapp")
}
