package jobpostings

import (
	"context"
)

// GetYouJobPostings finds JobPostings found at https://jobs.lever.co/you
func GetYouJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "you")
}
