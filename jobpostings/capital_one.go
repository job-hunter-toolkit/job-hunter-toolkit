package jobpostings

import (
	"context"
)

// GetCapitalOneJobPostings finds JobPostings using https://capitalone.wd1.myworkdayjobs.com/en-US/Capital_One
func GetCapitalOneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://capitalone.wd1.myworkdayjobs.com/en-US/Capital_One")
}