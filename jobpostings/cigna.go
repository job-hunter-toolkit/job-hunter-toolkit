package jobpostings

import (
	"context"
)

// GetCignaJobPostings finds JobPostings using https://cigna.wd5.myworkdayjobs.com/cignacareers/
func GetCignaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://cigna.wd5.myworkdayjobs.com/cignacareers/")
}
