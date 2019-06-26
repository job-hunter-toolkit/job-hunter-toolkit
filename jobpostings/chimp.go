package jobpostings

import (
	"context"
)

// GetChimpJobPostings finds JobPostings found at https://chimp.bamboohr.com/jobs/
func GetChimpJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "chimp")
}
