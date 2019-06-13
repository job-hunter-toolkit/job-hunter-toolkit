package jobpostings

import (
	"context"
)

// GetUniregistryJobPostings finds JobPostings found at https://itc.bamboohr.com/jobs/
func GetUniregistryJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "itc")
}
