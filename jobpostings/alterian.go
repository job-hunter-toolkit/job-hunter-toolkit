package jobpostings

import (
	"context"
)

// GetAlterianJobPostings finds JobPostings found at https://alterian.bamboohr.com/jobs/
func GetAlterianJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "alterian")
}
