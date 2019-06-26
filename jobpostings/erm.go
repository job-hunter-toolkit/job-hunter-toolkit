package jobpostings

import (
	"context"
)

// GetERMJobPostings finds JobPostings using https://erm.wd3.myworkdayjobs.com/ERM_Careers
func GetERMJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://erm.wd3.myworkdayjobs.com/ERM_Careers")
}
