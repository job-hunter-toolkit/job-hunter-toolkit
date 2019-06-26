package jobpostings

import (
	"context"
)

// GetRollsRoyceJobPostings finds JobPostings found at https://rollsroyce.wd3.myworkdayjobs.com/professional
func GetRollsRoyceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://rollsroyce.wd3.myworkdayjobs.com/professional")
}
