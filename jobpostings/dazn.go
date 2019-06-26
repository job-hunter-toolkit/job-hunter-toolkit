package jobpostings

import (
	"context"
)

// GetDaznJobPostings finds JobPostings found at https:/lever.co
func GetDaznJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "dazn")
}
