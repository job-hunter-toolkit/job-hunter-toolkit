package jobpostings

import (
	"context"
)

// GetCocaColaJobPostings finds JobPostings using https://coke.wd1.myworkdayjobs.com/coca-cola-careers
func GetCocaColaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://coke.wd1.myworkdayjobs.com/coca-cola-careers")
}
