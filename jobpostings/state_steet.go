package jobpostings

import (
	"context"
)

// GetStateStreetJobPostings finds JobPostings using https://statestreet.wd1.myworkdayjobs.com/Global
func GetStateStreetJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://statestreet.wd1.myworkdayjobs.com/Global")
}
