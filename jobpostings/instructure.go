package jobpostings

import (
	"context"
)

// GetInstructureJobPostings finds JobPostings found at https:/lever.co
func GetInstructureJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "instructure")
}
