package jobpostings

import (
	"context"
)

// GetSecuronixJobPostings finds JobPostings found at https://securonix.bamboohr.com/jobs/
func GetSecuronixJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "securonix")
}
