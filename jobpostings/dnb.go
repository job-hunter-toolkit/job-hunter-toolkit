package jobpostings

import (
	"context"
)

// GetDnBJobPostings finds JobPostings using https://dnb.wd1.myworkdayjobs.com/Careers
func GetDnBJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://dnb.wd1.myworkdayjobs.com/Careers")
}
