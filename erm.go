package jabba

import (
	"context"
)

// GetERMJobPostings finds JobPostings using https://erm.wd3.myworkdayjobs.com/ERM_Careers
func GetERMJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://erm.wd3.myworkdayjobs.com/ERM_Careers")
}