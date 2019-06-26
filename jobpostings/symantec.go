package jobpostings

import (
	"context"
)

// GetSymantecJobPostings finds JobPostings found at https://symantec.wd1.myworkdayjobs.com/careers
func GetSymantecJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://symantec.wd1.myworkdayjobs.com/careers")
}
