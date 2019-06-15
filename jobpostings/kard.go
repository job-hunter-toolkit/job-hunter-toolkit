package jobpostings

import (
	"context"
)

// GetKardJobPostings finds JobPostings found at https:/lever.co
func GetKardJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "getkard")
}
