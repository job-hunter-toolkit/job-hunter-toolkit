package jobpostings

import (
	"context"
)

// GetKohlsJobPostings finds JobPostings using https://kohls.wd1.myworkdayjobs.com/kohlscareers
func GetKohlsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://kohls.wd1.myworkdayjobs.com/kohlscareers")
}
