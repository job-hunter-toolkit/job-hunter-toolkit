package jobpostings

import (
	"context"
)

// GetModsyJobPostings finds JobPostings found at https://modsy.bamboohr.com/jobs/
func GetModsyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "modsy")
}
