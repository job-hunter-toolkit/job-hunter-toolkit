package jobpostings

import (
	"context"
)

// GetMetalToadJobPostings finds JobPostings found at https://metaltoad.bamboohr.com/jobs/
func GetMetalToadJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "metaltoad")
}
