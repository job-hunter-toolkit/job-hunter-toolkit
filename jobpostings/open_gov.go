package jobpostings

import (
	"context"
)

// GetOpenGovJobPostings finds JobPostings found at https:/lever.co
func GetOpenGovJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "opengov")
}
