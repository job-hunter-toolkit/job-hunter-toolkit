package jobpostings

import (
	"context"
)

// GetNPMJobPostings finds JobPostings found at https:/lever.co
func GetNPMJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "npm")
}
