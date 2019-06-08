package jobpostings

import (
	"context"
)

// GetPfizerJobPostings finds JobPostings found at https://pfizer.wd1.myworkdayjobs.com/PfizerCareers
func GetPfizerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://pfizer.wd1.myworkdayjobs.com/PfizerCareers")
}
