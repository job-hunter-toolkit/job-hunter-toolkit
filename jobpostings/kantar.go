package jobpostings

import (
	"context"
)

// GetKantarJobPostings finds JobPostings using https://kantar.wd3.myworkdayjobs.com/en-US/KANTAR
func GetKantarJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://kantar.wd3.myworkdayjobs.com/en-US/KANTAR")
}
