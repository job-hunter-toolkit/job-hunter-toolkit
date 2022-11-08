package jobpostings

import (
	"context"
)

// GetPPFAJobPostings finds JobPostings found at https://jobs.lever.co/ppfa
func GetPPFAJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "ppfa")
}
