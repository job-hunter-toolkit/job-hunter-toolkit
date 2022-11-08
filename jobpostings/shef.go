package jobpostings

import (
	"context"
)

// GetShefJobPostings finds JobPostings found at https://jobs.lever.co/shef
func GetShefJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "shef")
}
