package jobpostings

import (
	"context"
)

// GetXeroJobPostings finds JobPostings found at https://jobs.lever.co/xero
func GetXeroJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "xero")
}
