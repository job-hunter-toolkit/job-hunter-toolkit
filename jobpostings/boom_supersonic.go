package jobpostings

import (
	"context"
)

// GetBoomSupersonicJobPostings finds JobPostings found at https:/lever.co
func GetBoomSupersonicJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "boom")
}
