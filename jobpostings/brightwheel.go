package jobpostings

import (
	"context"
)

// GetBrightwheelJobPostings finds JobPostings found at https:/lever.co
func GetBrightwheelJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "brightwheel")
}
