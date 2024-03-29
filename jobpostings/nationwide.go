package jobpostings

import (
	"context"
)

// GetNationwideJobPostings finds JobPostings using https://nationwide.wd1.myworkdayjobs.com/Nationwide_Career
func GetNationwideJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://nationwide.wd1.myworkdayjobs.com/Nationwide_Career")
}
