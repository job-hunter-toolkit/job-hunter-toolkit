package jobpostings

import (
	"context"
)

// GetHackerOneJobPostings finds JobPostings found at https:/lever.co
func GetHackerOneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "hackerone")
}
