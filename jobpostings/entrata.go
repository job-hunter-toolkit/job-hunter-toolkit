package jobpostings

import (
	"context"
)

// GetEntrataJobPostings finds JobPostings found at https://jobs.lever.co/entrata
func GetEntrataJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "entrata")
}
