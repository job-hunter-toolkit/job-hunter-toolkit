package jobpostings

import (
	"context"
)

// GetVerizonMediaJobPostings finds JobPostings found at https://oath.wd5.myworkdayjobs.com/careers
func GetVerizonMediaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://oath.wd5.myworkdayjobs.com/careers")
}
