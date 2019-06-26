package jobpostings

import (
	"context"
)

// GetEssentialJobPostings finds JobPostings found at https:/lever.co
func GetEssentialJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "essential")
}
