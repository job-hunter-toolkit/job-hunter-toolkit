package jobpostings

import (
	"context"
)

// GetSaltStackJobPostings finds JobPostings found at https://saltstack.bamboohr.com/jobs/
func GetSaltStackJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "saltstack")
}
