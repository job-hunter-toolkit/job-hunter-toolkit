package jobpostings

import (
	"context"
)

// GetSwatJobPostings finds JobPostings found at https://swat.bamboohr.com/jobs/
func GetSwatJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "swat")
}
