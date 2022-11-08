package jobpostings

import (
	"context"
)

// GetWorkrampJobPostings finds JobPostings found at https://jobs.lever.co/workramp
func GetWorkrampJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "workramp")
}
