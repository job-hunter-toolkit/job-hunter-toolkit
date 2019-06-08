package jabba

import (
	"context"
)

// GetDnBJobPostings finds JobPostings using https://dnb.wd1.myworkdayjobs.com/Careers
func GetDnBJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://dnb.wd1.myworkdayjobs.com/Careers")
}