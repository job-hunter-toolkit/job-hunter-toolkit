package jobpostings

import (
	"context"
)

// GetWayfairJobPostings finds JobPostings found at https:/jibeapply.com
func GetWayfairJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "wayfair")
}
