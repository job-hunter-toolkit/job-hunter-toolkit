package jobpostings

import (
	"context"
)

// GetMotorolaSolutionsJobPostings finds JobPostings using https://motorolasolutions.wd5.myworkdayjobs.com/en-US/Careers/
func GetMotorolaSolutionsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://motorolasolutions.wd5.myworkdayjobs.com/en-US/Careers/")
}
