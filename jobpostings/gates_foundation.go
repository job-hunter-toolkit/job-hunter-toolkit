package jobpostings

import (
	"context"
)

// GetGatesFoundationJobPostings finds JobPostings using https://gatesfoundation.wd1.myworkdayjobs.com/Gates
func GetGatesFoundationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://gatesfoundation.wd1.myworkdayjobs.com/Gates")
}
