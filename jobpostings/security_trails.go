package jobpostings

import (
	"context"
)

// GetSecurityTrailsJobPostings finds JobPostings found at https://sthr.bamboohr.com/jobs/
func GetSecurityTrailsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "sthr")
}
