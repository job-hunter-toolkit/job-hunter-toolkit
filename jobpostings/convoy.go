package jobpostings

import (
	"context"
)

// GetConvoyJobPostings finds JobPostings found at https:/lever.co
func GetConvoyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "convoy")
}
