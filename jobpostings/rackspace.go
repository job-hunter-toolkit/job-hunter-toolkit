package jobpostings

import (
	"context"
)

// GetRackspaceJobPostings finds JobPostings found at https://jobs.lever.co/rackspace
func GetRackspaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "rackspace")
}
