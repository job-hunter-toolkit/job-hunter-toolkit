package jobpostings

import (
	"context"
)

// GetCamblyJobPostings finds JobPostings found at https:/lever.co
func GetCamblyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "cambly")
}
