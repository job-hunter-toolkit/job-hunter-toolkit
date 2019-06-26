package jobpostings

import (
	"context"
)

// GetRoverJobPostings finds JobPostings found at https:/lever.co
func GetRoverJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "rover")
}
