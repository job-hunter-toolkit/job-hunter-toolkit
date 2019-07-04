package jobpostings

import (
	"context"
)

// GetBankOfAmericaJobPostings finds JobPostings using https://ghr.wd1.myworkdayjobs.com/en-US/lateral-us
func GetBankOfAmericaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://ghr.wd1.myworkdayjobs.com/en-US/lateral-us")
}
