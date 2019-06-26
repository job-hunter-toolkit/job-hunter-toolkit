package jobpostings

import (
	"context"
)

// GetIronOxJobPostings finds JobPostings found at https:/lever.co
func GetIronOxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "ironox")
}
