package jobpostings

import (
	"context"
)

// GetEvergrowJobPostings finds JobPostings found at https://jobs.lever.co/Evergrow
func GetEvergrowJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "Evergrow")
}
