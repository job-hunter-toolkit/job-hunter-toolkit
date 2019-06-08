package jobpostings

import (
	"context"
)

// GetGatesFoundationJobPostings finds JobPostings using https://gatesfoundation.wd1.myworkdayjobs.com/Gates
func GetGatesFoundationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://gatesfoundation.wd1.myworkdayjobs.com/Gates")
}
