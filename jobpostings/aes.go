package jobpostings

import (
	"context"
)

// GetAESJobPostings finds JobPostings using https://aes.wd1.myworkdayjobs.com/AES_US/jobs
func GetAESJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://aes.wd1.myworkdayjobs.com/AES_US/jobs")
}
