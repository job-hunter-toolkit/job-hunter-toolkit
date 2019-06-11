package jobpostings

import (
	"context"
)

// GetXylemJobPostings finds JobPostings found at https://jobvite.com
func GetXylemJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(context.Background(), "xylem")
}
