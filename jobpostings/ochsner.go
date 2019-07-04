package jobpostings

import (
	"context"
)

// GetOchsnerJobPostings finds JobPostings using https://ochsner.wd1.myworkdayjobs.com/en-US/Ochsner
func GetOchsnerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://ochsner.wd1.myworkdayjobs.com/en-US/Ochsner")
}
