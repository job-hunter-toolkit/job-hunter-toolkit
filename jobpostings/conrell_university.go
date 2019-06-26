package jobpostings

import (
	"context"
)

// GetCornellUniversityJobPostings finds JobPostings using https://cornell.wd1.myworkdayjobs.com/CCECareerPage
func GetCornellUniversityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://cornell.wd1.myworkdayjobs.com/CCECareerPage")
}
