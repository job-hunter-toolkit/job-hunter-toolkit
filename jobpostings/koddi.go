package jobpostings

import (
	"context"
)

// GetKoddiJobPostings finds JobPostings found at https:/lever.co
func GetKoddiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "koddi")
}
