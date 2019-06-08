package jabba

import (
	"context"
)

// GetStateStreetJobPostings finds JobPostings using https://statestreet.wd1.myworkdayjobs.com/Global
func GetStateStreetJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://statestreet.wd1.myworkdayjobs.com/Global")
}