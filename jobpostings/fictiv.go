package jobpostings

import (
	"context"
)

// GetFictivJobPostings finds JobPostings found at https:/lever.co
func GetFictivJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "fictiv")
}
