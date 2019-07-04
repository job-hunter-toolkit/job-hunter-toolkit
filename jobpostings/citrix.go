package jobpostings

import (
	"context"
)

// GetCitrixJobPostings finds JobPostings using https://citrix.wd1.myworkdayjobs.com/CitrixCareers
func GetCitrixJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://citrix.wd1.myworkdayjobs.com/CitrixCareers")
}