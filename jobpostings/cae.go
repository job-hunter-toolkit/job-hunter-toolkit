package jobpostings

import (
	"context"
)

// GetCAEJobPostings finds JobPostings found at https://cae.wd3.myworkdayjobs.com/career/jobs
func GetCAEJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://cae.wd3.myworkdayjobs.com/career/jobs")
}
