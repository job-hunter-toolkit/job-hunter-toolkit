package jobpostings

import (
	"context"
)

// GetEarlyWarningJobPostings finds JobPostings found at https://earlywarning.wd5.myworkdayjobs.com/earlywarningcareers1
func GetEarlyWarningJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://earlywarning.wd5.myworkdayjobs.com/earlywarningcareers1")
}
