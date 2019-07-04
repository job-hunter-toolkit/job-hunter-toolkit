package jobpostings

import (
	"context"
)

// GetUniversityOfChicagoJobPostings finds JobPostings using https://uchicago.wd5.myworkdayjobs.com/en-US/External
func GetUniversityOfChicagoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://uchicago.wd5.myworkdayjobs.com/en-US/External")
}
