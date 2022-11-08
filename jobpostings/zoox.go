package jobpostings

import (
	"context"
)

// GetZooxJobPostings finds JobPostings found at https://jobs.lever.co/zoox
func GetZooxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "zoox")
}
