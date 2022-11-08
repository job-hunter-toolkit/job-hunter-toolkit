package jobpostings

import (
	"context"
)

// GetEmeritusJobPostings finds JobPostings found at https://jobs.lever.co/emeritus
func GetEmeritusJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "emeritus")
}
