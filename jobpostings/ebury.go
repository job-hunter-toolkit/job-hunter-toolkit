package jobpostings

import (
	"context"
)

// GetEburyJobPostings finds JobPostings found at https:/lever.co
func GetEburyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "ebury")
}
