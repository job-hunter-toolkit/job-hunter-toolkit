package jobpostings

import (
	"context"
)

// GetCoupaJobPostings finds JobPostings found at https:/lever.co
func GetCoupaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "coupa")
}
