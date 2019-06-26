package jobpostings

import (
	"context"
)

// GetPAEJobPostings finds JobPostings found at https://pae.wd1.myworkdayjobs.com/PAE_Careers
func GetPAEJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://pae.wd1.myworkdayjobs.com/PAE_Careers")
}
