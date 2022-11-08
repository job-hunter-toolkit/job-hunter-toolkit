package jobpostings

import (
	"context"
)

// GetFanaticsJobPostings finds JobPostings found at https://jobs.lever.co/fanatics
func GetFanaticsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "fanatics")
}
