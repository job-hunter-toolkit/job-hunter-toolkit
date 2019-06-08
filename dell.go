package jabba

import (
	"context"
)

// GetDellJobPostings finds JobPostings using https://dell.wd1.myworkdayjobs.com/External
func GetDellJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://dell.wd1.myworkdayjobs.com/External")
}