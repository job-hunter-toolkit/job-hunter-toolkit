package jobpostings

import (
	"context"
)

// GetCarbonBlackJobPostings finds JobPostings found at https://carbonblack.wd1.myworkdayjobs.com/Life_at_Cb
func GetCarbonBlackJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://carbonblack.wd1.myworkdayjobs.com/Life_at_Cb")
}