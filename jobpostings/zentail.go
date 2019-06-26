package jobpostings

import (
	"context"
)

// GetZentailJobPostings finds JobPostings found at https:/lever.co
func GetZentailJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "zentail")
}
