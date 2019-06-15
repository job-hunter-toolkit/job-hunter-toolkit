package jobpostings

import (
	"context"
)

// GetChangeJobPostings finds JobPostings found at https:/lever.co
func GetChangeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "change")
}
