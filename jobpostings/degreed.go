package jobpostings

import (
	"context"
)

// GetDegreedJobPostings finds JobPostings found at https:/lever.co
func GetDegreedJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "degreed")
}
