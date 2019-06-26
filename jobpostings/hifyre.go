package jobpostings

import (
	"context"
)

// GetHifyreJobPostings finds JobPostings found at https://hifyre.bamboohr.com/jobs/
func GetHifyreJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "hifyre")
}
