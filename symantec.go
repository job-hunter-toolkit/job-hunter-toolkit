package jabba

import (
	"context"
)

// GetSymantecJobPostings finds JobPostings found at https://symantec.wd1.myworkdayjobs.com/careers
func GetSymantecJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://symantec.wd1.myworkdayjobs.com/careers")
}