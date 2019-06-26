package jobpostings

import (
	"context"
)

// GetT1CGJobPostings finds JobPostings found at https://t1cg.bamboohr.com/jobs/
func GetT1CGJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "t1cg")
}
