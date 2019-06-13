package jobpostings

import (
	"context"
)

// GetRollPayJobPostings finds JobPostings found at https://rollpay.bamboohr.com/jobs/
func GetRollPayJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "rollpay")
}
