package jobpostings

import (
	"context"
)

// GetUniversityOfVirginiaJobPostings finds JobPostings using https://uva.wd1.myworkdayjobs.com/UVAJobs
func GetUniversityOfVirginiaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://uva.wd1.myworkdayjobs.com/UVAJobs")
}