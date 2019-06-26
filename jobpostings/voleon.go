package jobpostings

import (
	"context"
)

// GetVoleonJobPostings finds JobPostings found at https:/lever.co
func GetVoleonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "voleon")
}
