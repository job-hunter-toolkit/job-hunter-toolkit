package jobpostings

import (
	"context"
)

// GetIOTAJobPostings finds JobPostings found at https://iota.bamboohr.com/jobs/
func GetIOTAJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "iota")
}
