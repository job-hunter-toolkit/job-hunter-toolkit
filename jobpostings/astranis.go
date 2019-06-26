package jobpostings

import (
	"context"
)

// GetAstranisJobPostings finds JobPostings found at https:/lever.co
func GetAstranisJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "astranis")
}
