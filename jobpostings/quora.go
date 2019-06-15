package jobpostings

import (
	"context"
)

// GetQuoraJobPostings finds JobPostings found at https:/lever.co
func GetQuoraJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "quora")
}
