package jobpostings

import (
	"context"
)

// GetBigHealthJobPostings finds JobPostings found at https:/lever.co
func GetBigHealthJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "bighealth")
}
