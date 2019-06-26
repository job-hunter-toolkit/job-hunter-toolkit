package jobpostings

import (
	"context"
)

// GetVeritasJobPostings finds JobPostings found at https://veritas.wd1.myworkdayjobs.com/careers
func GetVeritasJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://veritas.wd1.myworkdayjobs.com/careers")
}
