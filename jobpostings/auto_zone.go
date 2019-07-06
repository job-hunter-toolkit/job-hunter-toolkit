package jobpostings

import (
	"context"
)

// GetAutoZoneJobPostings finds JobPostings found at https:/jibeapply.com
func GetAutoZoneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "autozone")
}
