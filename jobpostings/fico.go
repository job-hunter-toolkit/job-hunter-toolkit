package jobpostings

import (
	"context"
)

// GetFICOJobPostings finds JobPostings using https://fico.wd1.myworkdayjobs.com/External
func GetFICOJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://fico.wd1.myworkdayjobs.com/External")
}
