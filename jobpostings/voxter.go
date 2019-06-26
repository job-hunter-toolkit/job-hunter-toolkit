package jobpostings

import (
	"context"
)

// GetVoxterJobPostings finds JobPostings found at https://voxter.bamboohr.com/jobs/
func GetVoxterJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "voxter")
}
