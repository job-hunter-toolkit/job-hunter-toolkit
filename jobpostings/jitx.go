package jobpostings

import (
	"context"
)

// GetJitxJobPostings finds JobPostings found at https://jobs.lever.co/jitxinc
func GetJitxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "jitxinc")
}
