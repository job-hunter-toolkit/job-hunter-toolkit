package jabba

import (
	"context"
)

// GetRollsRoyceJobPostings finds JobPostings found at https://rollsroyce.wd3.myworkdayjobs.com/professional
func GetRollsRoyceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://rollsroyce.wd3.myworkdayjobs.com/professional")
}