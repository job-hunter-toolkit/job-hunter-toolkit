package jobpostings

import (
	"context"
)

// GetAcelityJobPostings finds JobPostings found at https:/jibeapply.com
func GetAcelityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "acelity")
}
