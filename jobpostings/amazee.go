package jobpostings

import (
	"context"
)

// GetAmazeeJobPostings finds JobPostings found at https://amazee.bamboohr.com/jobs/
func GetAmazeeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "amazee")
}
