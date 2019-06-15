package jobpostings

import (
	"context"
)

// GetSignalJobPostings finds JobPostings found at https:/lever.co
func GetSignalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "signal")
}
