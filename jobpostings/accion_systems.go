package jobpostings

import (
	"context"
)

// GetAccionSystemsJobPostings finds JobPostings found at https:/lever.co
func GetAccionSystemsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "accion-systems")
}
