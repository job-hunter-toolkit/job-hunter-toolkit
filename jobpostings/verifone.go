package jobpostings

import (
	"context"
)

// GetVerifoneJobPostings finds JobPostings found at https://jobvite.com
func GetVerifoneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "verifone")
}
