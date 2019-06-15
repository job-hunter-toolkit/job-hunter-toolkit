package jobpostings

import (
	"context"
)

// GetWellframeJobPostings finds JobPostings found at https:/lever.co
func GetWellframeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "wellframe")
}
