package jobpostings

import (
	"context"
)

// GetRelayrJobPostings finds JobPostings found at https://relayr.bamboohr.com/jobs/
func GetRelayrJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "relayr")
}
