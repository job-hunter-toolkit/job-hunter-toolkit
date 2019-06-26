package jobpostings

import (
	"context"
)

// GetColossalJobPostings finds JobPostings found at https://colossal.bamboohr.com/jobs/
func GetColossalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "colossal")
}
