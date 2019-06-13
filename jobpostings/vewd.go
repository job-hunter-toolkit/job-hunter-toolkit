package jobpostings

import (
	"context"
)

// GetVewdJobPostings finds JobPostings found at https://vewd.bamboohr.com/jobs/
func GetVewdJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "vewd")
}
