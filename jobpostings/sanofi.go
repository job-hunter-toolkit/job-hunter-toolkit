package jobpostings

import (
	"context"
)

// GetSanofiJobPostings finds JobPostings using https://sanofi.wd3.myworkdayjobs.com/SanofiCareers
func GetSanofiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://sanofi.wd3.myworkdayjobs.com/SanofiCareers")
}
