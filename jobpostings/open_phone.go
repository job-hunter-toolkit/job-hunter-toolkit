package jobpostings

import (
	"context"
)

// GetOpenPhoneJobPostings finds JobPostings found at https:/lever.co
func GetOpenPhoneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "openphone")
}
