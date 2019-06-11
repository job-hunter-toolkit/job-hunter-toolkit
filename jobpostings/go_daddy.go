package jobpostings

import (
	"context"
)

// GetGoDaddyJobPostings finds JobPostings found at https://godaddy.wd1.myworkdayjobs.com/GoDaddy_careers
func GetGoDaddyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://godaddy.wd1.myworkdayjobs.com/GoDaddy_careers")
}
