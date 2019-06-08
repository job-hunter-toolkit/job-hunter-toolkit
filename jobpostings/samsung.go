package jobpostings

import (
	"context"
)

// GetSamsungJobPostings finds JobPostings found at https://sec.wd3.myworkdayjobs.com/Samsung_Careers
func GetSamsungJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://sec.wd3.myworkdayjobs.com/Samsung_Careers")
}
