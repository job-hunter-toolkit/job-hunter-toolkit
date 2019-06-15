package jobpostings

import (
	"context"
)

// GetNYMediaJobPostings finds JobPostings found at https:/lever.co
func GetNYMediaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "nymedia")
}
