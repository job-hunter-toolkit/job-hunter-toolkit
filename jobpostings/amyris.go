package jobpostings

import (
	"context"
)

// GetAmyrisJobPostings finds JobPostings found at https:/lever.co
func GetAmyrisJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "amyris")
}
