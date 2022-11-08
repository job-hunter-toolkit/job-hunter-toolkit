package jobpostings

import (
	"context"
)

// GetPocketWorldsJobPostings finds JobPostings found at https://jobs.lever.co/pocketworlds
func GetPocketWorldsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "pocketworlds")
}
