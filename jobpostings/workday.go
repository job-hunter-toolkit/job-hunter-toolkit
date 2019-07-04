package jobpostings

import (
	"context"
)

// GetWorkdayJobPostings finds JobPostings using https://workday.wd5.myworkdayjobs.com/Workday
func GetWorkdayJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://workday.wd5.myworkdayjobs.com/Workday")
}