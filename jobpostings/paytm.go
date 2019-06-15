package jobpostings

import (
	"context"
)

// GetPaytmJobPostings finds JobPostings found at https:/lever.co
func GetPaytmJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "paytm")
}
