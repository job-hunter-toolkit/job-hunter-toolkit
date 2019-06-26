package jobpostings

import (
	"context"
)

// GetDellJobPostings finds JobPostings using https://dell.wd1.myworkdayjobs.com/External
func GetDellJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://dell.wd1.myworkdayjobs.com/External")
}
