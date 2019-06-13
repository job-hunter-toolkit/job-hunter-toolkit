package jobpostings

import (
	"context"
)

// GetZeroFoxJobPostings finds JobPostings found at https://zerofox.bamboohr.com/jobs/
func GetZeroFoxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "zerofox")
}
