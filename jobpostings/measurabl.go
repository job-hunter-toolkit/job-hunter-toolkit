package jobpostings

import (
	"context"
)

// GetMeasurablJobPostings finds JobPostings found at https://measurabl.bamboohr.com/jobs/
func GetMeasurablJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "measurabl")
}
