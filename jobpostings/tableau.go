package jobpostings

import (
	"context"
)

// GetTableauJobPostings finds JobPostings found at https://tableau.wd1.myworkdayjobs.com/External
func GetTableauJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://tableau.wd1.myworkdayjobs.com/External")
}
