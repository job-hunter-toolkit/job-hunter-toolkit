package jobpostings

import (
	"context"
)

// GetModernTimesBeerJobPostings finds JobPostings found at https:/lever.co
func GetModernTimesBeerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "moderntimesbeer")
}
