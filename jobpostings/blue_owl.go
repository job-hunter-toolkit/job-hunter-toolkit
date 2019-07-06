package jobpostings

import (
	"context"
)

// GetBlueOwlJobPostings finds JobPostings found at https:/lever.co
func GetBlueOwlJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "blueowl")
}
