package jobpostings

import (
	"context"
)

// GetPWCJobPostings finds JobPostings using https://pwc.wd3.myworkdayjobs.com/US_Experienced_Careers
func GetPWCJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://pwc.wd3.myworkdayjobs.com/US_Experienced_Careers")
}
