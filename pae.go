package jabba

import (
	"context"
)

// GetPAEJobPostings finds JobPostings found at https://pae.wd1.myworkdayjobs.com/PAE_Careers
func GetPAEJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://pae.wd1.myworkdayjobs.com/PAE_Careers")
}
