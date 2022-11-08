package jobpostings

import (
	"context"
)

// GetDragosJobPostings finds JobPostings found at https://jobs.lever.co/dragos
func GetDragosJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "dragos")
}
