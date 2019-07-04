package jobpostings

import (
	"context"
)

// GetBorgWarnerJobPostings finds JobPostings using https://borgwarner.wd5.myworkdayjobs.com/BorgWarner_Careers
func GetBorgWarnerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://borgwarner.wd5.myworkdayjobs.com/BorgWarner_Careers")
}
