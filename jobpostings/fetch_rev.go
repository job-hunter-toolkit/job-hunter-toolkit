package jobpostings

import (
	"context"
)

// GetFetchRevJobPostings finds JobPostings found at https://fetchrev.bamboohr.com/jobs/
func GetFetchRevJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "fetchrev")
}
