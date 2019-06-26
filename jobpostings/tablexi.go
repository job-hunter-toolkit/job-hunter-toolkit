package jobpostings

import (
	"context"
)

// GetTablexiJobPostings finds JobPostings found at https://tablexi.bamboohr.com/jobs/
func GetTablexiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "tablexi")
}
