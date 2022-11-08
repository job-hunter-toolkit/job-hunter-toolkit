package jobpostings

import (
	"context"
)

// GetCanvaJobPostings finds JobPostings found at https://jobs.lever.co/canva
func GetCanvaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "canva")
}
