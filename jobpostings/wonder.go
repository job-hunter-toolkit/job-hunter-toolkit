package jobpostings

import (
	"context"
)

// GetWonderJobPostings finds JobPostings found at https:/lever.co
func GetWonderJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "askwonder")
}
